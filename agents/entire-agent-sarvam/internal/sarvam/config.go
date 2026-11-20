package sarvam

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Sarvam's project config is hand-authored TOML that can carry anything, so
// hook install/uninstall edits it as lines rather than round-tripping a TOML
// parser: every line we do not own is preserved byte-for-byte, headers
// included — indentation and line endings stay as authored. Only the
// narrow grammar of hook declarations is understood:
//
//	[[hooks.<Event>]]            matcher group (body: optional matcher keys)
//	[[hooks.<Event>.hooks]]      hook entry (body: type/command/timeout/...)
//
// Entire-owned entries are recognized by their command carrying the
// "entire hooks <agent>" marker, the same convention the other external
// agents use.

const entireCommandMarker = "entire hooks " + AgentName

// kindGroup / kindHooks are headerEvent's block kinds: a matcher-group
// header ([[hooks.<Event>]]) versus a hook-entry block
// ([[hooks.<Event>.hooks]]).
const (
	kindGroup = "group"
	kindHooks = "hooks"
)

// configHookEntry is one hook handler we manage in the project config.
type configHookEntry struct {
	Event   string // PascalCase sarvam event key
	Name    string // stable logical name (used only for matching our entries)
	Command string // exact command string
}

type tomlBlock struct {
	header string   // raw header line, "" for the preamble block
	body   []string // lines after the header until the next header
}

// headerLinePattern matches a TOML table or array-of-tables header: opening
// brackets, a key without ] characters, matching closing brackets, and an
// optional trailing comment. Continuation lines of multi-line arrays or
// strings ("[\"a\", \"b\"],") do not match, so they stay in the body of the
// block that opened them instead of becoming phantom headers.
var headerLinePattern = regexp.MustCompile(`^\[+[^\]]*\]+\s*(?:#.*)?$`)

func isHeaderLine(line string) bool {
	return headerLinePattern.MatchString(line)
}

func headerEvent(header string) (event string, kind string) {
	trimmed := strings.TrimSpace(header)
	// A trailing comment is not part of the key: [[hooks.Stop]] # nightly.
	// Quoted keys containing '#' are unaffected in practice because the
	// events we manage are bare, unquoted names.
	if i := strings.Index(trimmed, "#"); i >= 0 {
		trimmed = strings.TrimSpace(trimmed[:i])
	}
	for _, prefix := range []string{"[[hooks.", "[hooks."} {
		if strings.HasPrefix(trimmed, prefix) {
			rest := strings.TrimSuffix(strings.TrimPrefix(trimmed, prefix), "]")
			rest = strings.TrimSuffix(rest, "]")
			switch {
			case strings.HasSuffix(rest, ".hooks"):
				return strings.TrimSuffix(rest, ".hooks"), kindHooks
			default:
				return rest, kindGroup
			}
		}
	}
	return "", ""
}

// netBracketsOutsideQuotes counts '[' minus ']' outside quoted strings and
// comments on a single line — enough to know whether the line leaves a
// multi-line array still open.
func netBracketsOutsideQuotes(line string) int {
	depth := 0
	var quote byte
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case quote != 0:
			if ch == '\\' && quote == '"' && i+1 < len(line) {
				i++ // skip the escaped character
				continue
			}
			if ch == quote {
				quote = 0
			}
		case ch == '"' || ch == '\'':
			quote = ch
		case ch == '#':
			return depth // comment runs to end of line
		case ch == '[':
			depth++
		case ch == ']':
			depth--
		}
	}
	return depth
}

// parseConfigBlocks splits the file into header-anchored blocks. The first
// block (preamble) has an empty header and holds everything before the first
// table header. Lines inside an open multi-line string (""" or ”') or an
// open multi-line array are always body lines — a line that merely looks
// like a header inside such a value must not split the block. Header lines
// are stored verbatim — indentation and line endings are the author's —
// and the returned flag records whether the file ended with a newline, so
// serialization round-trips byte-for-byte.
func parseConfigBlocks(content string) ([]tomlBlock, bool) {
	lines := strings.Split(content, "\n")
	// A trailing newline produces one empty final element; remember it so
	// serialization round-trips.
	trailingNewline := len(lines) > 0 && lines[len(lines)-1] == ""
	if trailingNewline {
		lines = lines[:len(lines)-1]
	}
	blocks := []tomlBlock{{}}
	multiLineString := "" // "" outside, otherwise the open delimiter
	arrayDepth := 0       // open multi-line arrays: body lines until closed
	for _, line := range lines {
		if multiLineString != "" {
			if strings.Count(line, multiLineString)%2 == 1 {
				multiLineString = ""
			}
			blocks[len(blocks)-1].body = append(blocks[len(blocks)-1].body, line)
			continue
		}
		if arrayDepth > 0 {
			blocks[len(blocks)-1].body = append(blocks[len(blocks)-1].body, line)
			arrayDepth += netBracketsOutsideQuotes(line)
			if arrayDepth < 0 {
				arrayDepth = 0
			}
			continue
		}
		if isHeaderLine(strings.TrimSpace(line)) {
			blocks = append(blocks, tomlBlock{header: line})
			continue
		}
		blocks[len(blocks)-1].body = append(blocks[len(blocks)-1].body, line)
		if depth := netBracketsOutsideQuotes(line); depth > 0 {
			arrayDepth = depth
			continue
		}
		for _, delimiter := range []string{`"""`, `'''`} {
			if strings.Count(line, delimiter)%2 == 1 {
				multiLineString = delimiter
				break
			}
		}
	}
	return blocks, trailingNewline
}

// serializeConfigBlocks is the inverse of parseConfigBlocks; when the file
// had no trailing newline, the last line keeps its missing newline.
func serializeConfigBlocks(blocks []tomlBlock, trailingNewline bool) string {
	var b strings.Builder
	for _, block := range blocks {
		if block.header != "" {
			b.WriteString(block.header)
			b.WriteByte('\n')
		}
		for _, line := range block.body {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	out := b.String()
	if !trailingNewline && strings.HasSuffix(out, "\n") {
		out = out[:len(out)-1]
	}
	return out
}

// entryCommand returns the command value of a hook-entry block, or "".
func entryCommand(block tomlBlock) string {
	for _, line := range block.body {
		key, value, ok := parseKeyValue(line)
		if ok && key == "command" {
			return value
		}
	}
	return ""
}

func entryIsCommandType(block tomlBlock) bool {
	for _, line := range block.body {
		key, value, ok := parseKeyValue(line)
		if ok && key == "type" && value == "command" {
			return true
		}
	}
	return false
}

// isEntireEntry reports whether a hook-entry block belongs to this adapter.
func isEntireEntry(block tomlBlock) bool {
	if !entryIsCommandType(block) {
		return false
	}
	command := entryCommand(block)
	return command != "" && strings.Contains(command, entireCommandMarker)
}

// parseKeyValue parses a single `key = value` TOML line, decoding basic and
// literal strings. Booleans and numbers come back raw. An unquoted trailing
// comment is stripped first: an entry line like `type = "command" # entire`
// must still be recognized as ours (otherwise the next install duplicates
// it), and a commented command line must decode to the exact command so a
// repeat install leaves the line — comment included — untouched.
func parseKeyValue(line string) (key string, value string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	eq := strings.Index(trimmed, "=")
	if eq < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(trimmed[:eq])
	value = strings.TrimSpace(stripTrailingComment(strings.TrimSpace(trimmed[eq+1:])))
	if len(value) >= 2 {
		if value[0] == '"' && value[len(value)-1] == '"' {
			return key, unescapeBasicString(value[1 : len(value)-1]), true
		}
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			return key, value[1 : len(value)-1], true
		}
	}
	return key, value, true
}

// stripTrailingComment removes an unquoted TOML comment from a value: the
// first '#' outside any quoted string ends the value. Quoted '#' characters
// survive. A value that never closes its quote keeps the '#' — malformed
// input is left as-is rather than guessed at.
func stripTrailingComment(value string) string {
	var quote byte
	for i := 0; i < len(value); i++ {
		ch := value[i]
		switch {
		case quote != 0:
			if ch == '\\' && quote == '"' && i+1 < len(value) {
				i++ // skip the escaped character
				continue
			}
			if ch == quote {
				quote = 0
			}
		case ch == '"' || ch == '\'':
			quote = ch
		case ch == '#':
			return strings.TrimRight(value[:i], " \t")
		}
	}
	return value
}

func unescapeBasicString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// encodeTOMLString renders s as a TOML basic string.
func encodeTOMLString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\t':
			b.WriteString("\\t")
		case '\r':
			b.WriteString("\\r")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func newEntryBlocks(event, command string) []tomlBlock {
	return []tomlBlock{
		{header: fmt.Sprintf("[[hooks.%s]]", event)},
		{header: fmt.Sprintf("[[hooks.%s.hooks]]", event), body: []string{
			"type = \"command\"",
			"command = " + encodeTOMLString(command),
		}},
	}
}

// repairParentlessHooks inserts the missing [[hooks.<Event>]] group headers
// the sarvam docs' example leads users to omit: without them the whole hooks
// table fails to parse ("invalid type: map, expected a sequence") and the CLI
// silently drops every hook. Inserting the header turns the map into the
// array of one matcher group, keeping the author's entries.
func repairParentlessHooks(blocks []tomlBlock) []tomlBlock {
	out := make([]tomlBlock, 0, len(blocks)+len(hookSpecs))
	seenGroup := map[string]bool{}
	for _, block := range blocks {
		event, kind := headerEvent(block.header)
		if event != "" && kind == kindHooks && !seenGroup[event] {
			if isManagedEvent(event) {
				out = append(out, tomlBlock{header: fmt.Sprintf("[[hooks.%s]]", event)})
				// One synthetic group header is the repair: every
				// parentless entry of the event joins it. Without this
				// mark, a second parentless entry would get its own
				// header and silently split the user's entries into
				// separate matcher groups.
				seenGroup[event] = true
			}
		}
		if event != "" && kind == kindGroup {
			seenGroup[event] = true
		}
		out = append(out, block)
	}
	return out
}

func isManagedEvent(event string) bool {
	for _, spec := range hookSpecs {
		if spec.SarvamEvent == event {
			return true
		}
	}
	return false
}

// checkNoMapFormEvents rejects the single-bracket table form for events we
// manage — the matcher-group header [hooks.<Event>] and the entry header
// [hooks.<Event>.hooks] alike: appending an array-of-tables next to a table
// definition is a TOML type conflict, and the CLI would silently drop every
// hook. Failing loudly beats corrupting a config the user must fix by hand
// anyway.
func checkNoMapFormEvents(blocks []tomlBlock) error {
	for _, block := range blocks {
		event, kind := headerEvent(block.header)
		if event == "" || !isManagedEvent(event) {
			continue
		}
		if kind != kindGroup && kind != kindHooks {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(block.header), "[[") {
			return fmt.Errorf(".sarvam/config.toml defines hooks.%s as a table (%s); the sarvam CLI requires arrays — replace the header with the [[...]] form and rerun", event, strings.TrimSpace(block.header))
		}
	}
	return nil
}

// upsertHookEntries installs the given entries, returning how many hook
// handlers were added or updated. Unrelated config content is untouched.
func upsertHookEntries(path string, entries []configHookEntry, force bool) (int, error) {
	blocks, trailingNewline, err := loadBlocks(path)
	if err != nil {
		return 0, err
	}
	if err := checkNoMapFormEvents(blocks); err != nil {
		return 0, err
	}
	original := serializeConfigBlocks(blocks, trailingNewline)
	if force {
		var removedEvents map[string]bool
		blocks, removedEvents = dropEntireEntries(blocks)
		// The dropped entries strand their [[hooks.<Event>]] group headers;
		// without this clean-up every force install would leave another
		// empty group behind.
		blocks = dropEmptyManagedGroups(blocks, removedEvents)
	}
	blocks = repairParentlessHooks(blocks)

	count := 0
	for _, entry := range entries {
		if upsertOne(&blocks, entry) {
			count++
		}
	}
	// Zero entries added or updated can still mean the blocks changed: a
	// parentless repair must reach the file, or the CLI keeps rejecting the
	// whole hooks table while install-hooks reports nothing happened. And
	// with no change at all, the author's bytes stay untouched.
	if count == 0 && serializeConfigBlocks(blocks, trailingNewline) == original {
		return 0, nil
	}
	if err := saveBlocks(path, blocks, trailingNewline); err != nil {
		return 0, err
	}
	return count, nil
}

func upsertOne(blocks *[]tomlBlock, entry configHookEntry) bool {
	// Update in place when an owned entry exists.
	for i, block := range *blocks {
		event, kind := headerEvent(block.header)
		if event != entry.Event || kind != kindHooks {
			continue
		}
		if !isEntireEntry(block) {
			continue
		}
		if entryCommand(block) == entry.Command {
			return false
		}
		for j, line := range block.body {
			if key, _, ok := parseKeyValue(line); ok && key == "command" {
				(*blocks)[i].body[j] = "command = " + encodeTOMLString(entry.Command)
				return true
			}
		}
	}

	// Insert after the event's last hook entry when one exists; otherwise
	// open a fresh group at the end of the hooks region (or EOF).
	insertAt := -1
	lastHooksRegion := -1
	for i, block := range *blocks {
		event, kind := headerEvent(block.header)
		if event == "" {
			continue
		}
		if kind == kindHooks && event == entry.Event {
			insertAt = i + 1
		}
		if trimmed := strings.TrimSpace(block.header); strings.HasPrefix(trimmed, "[hooks.") || strings.HasPrefix(trimmed, "[[hooks.") {
			lastHooksRegion = i + 1
		}
	}
	at := len(*blocks)
	if insertAt >= 0 {
		at = insertAt
	} else if lastHooksRegion >= 0 {
		at = lastHooksRegion
	}
	newBlocks := newEntryBlocks(entry.Event, entry.Command)
	out := make([]tomlBlock, 0, len(*blocks)+len(newBlocks))
	out = append(out, (*blocks)[:at]...)
	out = append(out, newBlocks...)
	out = append(out, (*blocks)[at:]...)
	*blocks = out
	return true
}

// removeHookEntries deletes every entry this adapter owns, plus the empty
// matcher-group headers those entries strand behind. A config holding none
// of our entries is left byte-for-byte untouched.
func removeHookEntries(path string) error {
	blocks, trailingNewline, err := loadBlocks(path)
	if err != nil {
		return err
	}
	if len(blocks) == 1 && len(blocks[0].body) == 0 && blocks[0].header == "" {
		return nil // no config at all
	}
	kept, removedEvents := dropEntireEntries(blocks)
	kept = dropEmptyManagedGroups(kept, removedEvents)
	if serializeConfigBlocks(kept, trailingNewline) == serializeConfigBlocks(blocks, trailingNewline) {
		// Nothing we own to remove: leave the author's bytes alone.
		return nil
	}
	return saveBlocks(path, kept, trailingNewline)
}

// dropEntireEntries deletes every entry this adapter owns and reports which
// events lost one, so the group cleanup that follows only removes headers
// our own entries stranded.
func dropEntireEntries(blocks []tomlBlock) ([]tomlBlock, map[string]bool) {
	removedEvents := map[string]bool{}
	out := make([]tomlBlock, 0, len(blocks))
	for _, block := range blocks {
		event, kind := headerEvent(block.header)
		if event != "" && kind == kindHooks && isEntireEntry(block) {
			removedEvents[event] = true
			continue
		}
		out = append(out, block)
	}
	return out, removedEvents
}

// dropEmptyManagedGroups removes [[hooks.<Event>]] group headers that our
// removed entries leave stranded: an empty group goes only when the event
// lost one of our owned entries in this pass and no same-event
// [[hooks.<Event>.hooks]] entry immediately follows it — the parent
// relationship TOML array-of-tables gives an entry. A group that still
// carries content (a matcher) always stays, and a group the user authored
// alone — including for legacy events an older release installed — is
// theirs, never ours to clean.
func dropEmptyManagedGroups(blocks []tomlBlock, removedEvents map[string]bool) []tomlBlock {
	out := make([]tomlBlock, 0, len(blocks))
	for i, block := range blocks {
		event, kind := headerEvent(block.header)
		if event != "" && kind == kindGroup && removedEvents[event] &&
			len(block.body) == 0 && !followedBySameEventEntry(blocks, i) {
			continue
		}
		out = append(out, block)
	}
	return out
}

// followedBySameEventEntry reports whether the block after blocks[i] is a
// hook entry of the same event. parseConfigBlocks opens every header its own
// block, so the next block is exactly where an entry's parent group ends.
func followedBySameEventEntry(blocks []tomlBlock, groupIndex int) bool {
	if groupIndex+1 >= len(blocks) {
		return false
	}
	groupEvent, _ := headerEvent(blocks[groupIndex].header)
	event, kind := headerEvent(blocks[groupIndex+1].header)
	return event == groupEvent && kind == kindHooks
}

func hookEntryInstalled(path, event, command string) bool {
	blocks, _, err := loadBlocks(path)
	if err != nil {
		return false
	}
	// An entry without its [[hooks.<Event>]] matcher-group header is not
	// installed in any way the CLI can parse — the whole hooks table fails
	// with "invalid type: map, expected a sequence" — so the group header
	// must precede the entry for it to count. A single-bracket group is a
	// table form the CLI rejects equally, so it does not count either.
	seenGroup := false
	for _, block := range blocks {
		e, kind := headerEvent(block.header)
		if e == "" {
			continue
		}
		if kind == kindGroup && e == event {
			seenGroup = strings.HasPrefix(strings.TrimSpace(block.header), "[[")
			continue
		}
		if kind == kindHooks && e == event && seenGroup &&
			isEntireEntry(block) && entryCommand(block) == command {
			return true
		}
	}
	return false
}

func loadBlocks(path string) ([]tomlBlock, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []tomlBlock{{}}, true, nil
	}
	if err != nil {
		return nil, true, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return []tomlBlock{{}}, true, nil
	}
	blocks, trailingNewline := parseConfigBlocks(string(data))
	return blocks, trailingNewline, nil
}

func saveBlocks(path string, blocks []tomlBlock, trailingNewline bool) error {
	content := serializeConfigBlocks(blocks, trailingNewline)
	// filepath.Dir is separator-aware: on Windows the config path carries
	// backslashes, where a hand-rolled "/" split would return "." and leave
	// the .sarvam directory uncreated.
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if existing, err := os.Stat(path); err == nil {
		mode = existing.Mode().Perm()
	}
	return os.WriteFile(path, []byte(content), mode)
}
