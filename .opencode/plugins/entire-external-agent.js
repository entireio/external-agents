import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const projectRoot = path.resolve(__dirname, '..', '..');
const skillPath = path.join(
  projectRoot,
  '.claude',
  'skills',
  'entire-external-agent',
  'SKILL.md'
);

export const EntireExternalAgentPlugin = async () => {
  const bootstrap = `<EXTERNAL_AGENT_SKILL>
The \`entire-external-agent\` skill is available in this project.

Use OpenCode's native \`skill\` tool to load the skill when the user wants to research, scaffold, implement, or validate an Entire CLI external agent.

Skill path: \`${skillPath}\`

Available phases:
- Full pipeline: \`${path.join(projectRoot, '.claude/skills/entire-external-agent/SKILL.md')}\`
- Research: \`${path.join(projectRoot, '.claude/skills/entire-external-agent/researcher.md')}\`
- Write tests: \`${path.join(projectRoot, '.claude/skills/entire-external-agent/test-writer.md')}\`
- Implement: \`${path.join(projectRoot, '.claude/skills/entire-external-agent/implementer.md')}\`

Tool mapping:
- \`TodoWrite\` -> \`update_plan\`
- subagent-oriented instructions -> OpenCode's native subagent features
- \`Skill\` tool -> OpenCode's native \`skill\` tool
- file and shell operations -> native OpenCode tools
</EXTERNAL_AGENT_SKILL>`;

  return {
    'experimental.chat.system.transform': async (_input, output) => {
      (output.system ||= []).push(bootstrap);
    }
  };
};
