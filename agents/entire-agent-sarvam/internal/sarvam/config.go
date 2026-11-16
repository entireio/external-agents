package sarvam

import (
	"fmt"
	"os"
	"strings"
)

// Sarvam's project config is hand-authored TOML that can carry anything, so
// hook install/uninstall edits it as lines rather than round-tripping a TOML
// parser: every line we do not own is preserved byte-for-byte. Only the
// narrow grammar of hook declarations is understood:
//
//	[[hooks.<Event>]]            matcher group (body: optional matcher keys)
//	[[hooks.<Event>.hooks]]      hook entry (body: type/command/timeout/...)
//
// Entire-owned entries are recognized by their command carrying the
// "entire hooks <agent>" marker, the same convention the other external
// agents use.

const entireCommandMarker = "entire hooks " + AgentName

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

func isHeaderLine(line string) bool {
	return strings.HasPrefix(line, "[")
}

func headerEvent(header string) (event string, kind string) {
	trimmed := strings.TrimSpace(header)
	for _, prefix := range []string{"[[hooks.", "[hooks."} {
		if strings.HasPrefix(trimmed, prefix) {
			rest := strings.TrimSuffix(strings.TrimPrefix(trimmed, prefix), "]")
			rest = strings.TrimSuffix(rest, "]")
			switch {
			case strings.HasSuffix(rest, ".hooks"):
				return strings.TrimSuffix(rest, ".hooks"), "hooks"
			default:
				return rest, "group"
			}
		}
	}
	return "", ""
}

// parseConfigBlocks splits the file into header-anchored blocks. The first
// block (preamble) has an empty header and holds everything before the first
// table header.
func parseConfigBlocks(content string) []tomlBlock {
	lines := strings.Split(content, "\n")
	// A trailing newline produces one empty final element; remember it so
	// serialization round-trips.
	trailingNewline := len(lines) > 0 && lines[len(lines)-1] == ""
	if trailingNewline {
		lines = lines[:len(lines)-1]
	}
	blocks := []tomlBlock{{}}
	for _, line := range lines {
		if isHeaderLine(strings.TrimSpace(line)) {
			blocks = append(blocks, tomlBlock{header: strings.TrimSpace(line)})
			continue
		}
		blocks[len(blocks)-1].body = append(blocks[len(blocks)-1].body, line)
	}
	return blocks
}

func serializeConfigBlocks(blocks []tomlBlock) string {
	var b strings.Builder
	for i, block := range blocks {
		if i > 0 {
			b.WriteByte('\n')
		}
		if block.header != "" {
			b.WriteString(block.header)
			b.WriteByte('\n')
		}
		for _, line := range block.body {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
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
// literal strings. Booleans and numbers come back raw.
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
	value = strings.TrimSpace(trimmed[eq+1:])
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
			fmt.Sprintf("command = %s", encodeTOMLString(command)),
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
		if event != "" && kind == "hooks" && !seenGroup[event] {
			if isManagedEvent(event) {
				out = append(out, tomlBlock{header: fmt.Sprintf("[[hooks.%s]]", event)})
			}
		}
		if event != "" && kind == "group" {
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

// checkNoMapFormEvents rejects the single-bracket map form for events we
// manage. Appending an array-of-tables next to a table definition is a TOML
// type conflict; failing loudly beats corrupting a config the user must fix
// by hand anyway.
func checkNoMapFormEvents(blocks []tomlBlock) error {
	for _, block := range blocks {
		event, kind := headerEvent(block.header)
		if event != "" && kind == "group" && isManagedEvent(event) && !strings.HasPrefix(block.header, "[[") {
			return fmt.Errorf(".sarvam/config.toml defines [hooks.%s] as a table; the sarvam CLI requires an array of matcher groups — replace the header with [[hooks.%s]] and rerun", event, event)
		}
	}
	return nil
}

// upsertHookEntries installs the given entries, returning how many hook
// handlers were added or updated. Unrelated config content is untouched.
func upsertHookEntries(path string, entries []configHookEntry, force bool) (int, error) {
	blocks, err := loadBlocks(path)
	if err != nil {
		return 0, err
	}
	if err := checkNoMapFormEvents(blocks); err != nil {
		return 0, err
	}
	if force {
		blocks = dropEntireEntries(blocks)
	}
	blocks = repairParentlessHooks(blocks)

	count := 0
	for _, entry := range entries {
		changed, err := upsertOne(&blocks, entry)
		if err != nil {
			return 0, err
		}
		if changed {
			count++
		}
	}
	if count == 0 {
		return 0, nil
	}
	if err := saveBlocks(path, blocks); err != nil {
		return 0, err
	}
	return count, nil
}

func upsertOne(blocks *[]tomlBlock, entry configHookEntry) (bool, error) {
	// Update in place when an owned entry exists.
	for i, block := range *blocks {
		event, kind := headerEvent(block.header)
		if event != entry.Event || kind != "hooks" {
			continue
		}
		if !isEntireEntry(block) {
			continue
		}
		if entryCommand(block) == entry.Command {
			return false, nil
		}
		for j, line := range block.body {
			if key, _, ok := parseKeyValue(line); ok && key == "command" {
				(*blocks)[i].body[j] = fmt.Sprintf("command = %s", encodeTOMLString(entry.Command))
				return true, nil
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
		if kind == "hooks" && event == entry.Event {
			insertAt = i + 1
		}
		if strings.HasPrefix(block.header, "[hooks.") || strings.HasPrefix(block.header, "[[hooks.") {
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
	return true, nil
}

// removeHookEntries deletes every entry this adapter owns.
func removeHookEntries(path string, specs []hookSpec) error {
	blocks, err := loadBlocks(path)
	if err != nil {
		return err
	}
	if len(blocks) == 1 && len(blocks[0].body) == 0 && blocks[0].header == "" {
		return nil // no config at all
	}
	kept := dropEntireEntries(blocks)
	kept = dropEmptyManagedGroups(kept)
	return saveBlocks(path, kept)
}

func dropEntireEntries(blocks []tomlBlock) []tomlBlock {
	out := make([]tomlBlock, 0, len(blocks))
	for _, block := range blocks {
		event, kind := headerEvent(block.header)
		if event != "" && kind == "hooks" && isEntireEntry(block) {
			continue
		}
		out = append(out, block)
	}
	return out
}

// dropEmptyManagedGroups removes [[hooks.<Event>]] group headers for our
// events whose body is empty after entry removal. Groups with content and any
// other table are kept.
func dropEmptyManagedGroups(blocks []tomlBlock) []tomlBlock {
	out := make([]tomlBlock, 0, len(blocks))
	for _, block := range blocks {
		event, kind := headerEvent(block.header)
		if event != "" && kind == "group" && isManagedEvent(event) && len(block.body) == 0 {
			continue
		}
		out = append(out, block)
	}
	return out
}

func hookEntryInstalled(path, event, name, command string) bool {
	blocks, err := loadBlocks(path)
	if err != nil {
		return false
	}
	blocks = repairParentlessHooks(blocks)
	for _, block := range blocks {
		e, kind := headerEvent(block.header)
		if e != event || kind != "hooks" {
			continue
		}
		if isEntireEntry(block) && entryCommand(block) == command {
			return true
		}
	}
	return false
}

func loadBlocks(path string) ([]tomlBlock, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []tomlBlock{{}}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return []tomlBlock{{}}, nil
	}
	return parseConfigBlocks(string(data)), nil
}

func saveBlocks(path string, blocks []tomlBlock) error {
	content := serializeConfigBlocks(blocks)
	if err := os.MkdirAll(mustDir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if existing, err := os.Stat(path); err == nil {
		mode = existing.Mode().Perm()
	}
	return os.WriteFile(path, []byte(content), mode)
}

func mustDir(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "."
	}
	return path[:idx]
}
