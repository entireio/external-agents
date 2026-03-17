import os from 'os';
import path from 'path';

const normalizePath = (value, homeDir) => {
  if (!value || typeof value !== 'string') {
    return null;
  }

  const trimmed = value.trim();
  if (!trimmed) {
    return null;
  }

  if (trimmed === '~') {
    return homeDir;
  }

  if (trimmed.startsWith('~/')) {
    return path.join(homeDir, trimmed.slice(2));
  }

  return path.resolve(trimmed);
};

export const EntireExternalAgentPlugin = async () => {
  const homeDir = os.homedir();
  const envConfigDir = normalizePath(process.env.OPENCODE_CONFIG_DIR, homeDir);
  const configDir = envConfigDir || path.join(homeDir, '.config/opencode');
  const skillPath = path.join(
    configDir,
    'skills',
    'external-agents',
    'entire-external-agent',
    'SKILL.md'
  );

  const bootstrap = `<EXTERNAL_AGENT_SKILL>
The \`entire-external-agent\` skill is installed for this environment.

Use OpenCode's native \`skill\` tool to load \`external-agents/entire-external-agent\` when the user wants to research, scaffold, implement, or validate an Entire CLI external agent.

Installed skill path: \`${skillPath}\`

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
