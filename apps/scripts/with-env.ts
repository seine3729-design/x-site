import { spawn } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const environments = ['development', 'production'] as const;

type AppEnvironment = (typeof environments)[number];

function isAppEnvironment(value: string): value is AppEnvironment {
	return environments.includes(value as AppEnvironment);
}

function applyEnvFile(path: string) {
	if (!existsSync(path)) {
		return;
	}

	for (const line of readFileSync(path, 'utf8').split('\n')) {
		const trimmed = line.trim();

		if (!trimmed || trimmed.startsWith('#')) {
			continue;
		}

		const index = trimmed.indexOf('=');

		if (index === -1) {
			continue;
		}

		const key = trimmed.slice(0, index).trim();

		if (!key) {
			continue;
		}

		let value = trimmed.slice(index + 1).trim();

		if (
			(value.startsWith('"') && value.endsWith('"')) ||
			(value.startsWith("'") && value.endsWith("'"))
		) {
			value = value.slice(1, -1);
		}

		process.env[key] = value;
	}
}

const appEnv = process.argv[2] ?? '';
const separator = process.argv.indexOf('--');
const command = separator === -1 ? [] : process.argv.slice(separator + 1);

if (!isAppEnvironment(appEnv) || command.length === 0) {
	console.error('用法: bun scripts/with-env.ts <development|production> -- <命令>');
	process.exit(1);
}

const joined = command.join(' ');
const needsBun14 = joined.includes('vite build') || joined.includes('./build');

if (needsBun14 && Bun.semver.order(Bun.version, '1.4.0') < 0) {
	console.error(`构建和启动需要 Bun 1.4 或更新版本。当前是 ${Bun.version}。`);
	process.exit(1);
}

const root = resolve(import.meta.dirname, '..');

applyEnvFile(resolve(root, `.env.${appEnv}`));
applyEnvFile(resolve(root, '.env.local'));
applyEnvFile(resolve(root, `.env.${appEnv}.local`));

process.env.APP_ENV = appEnv;
process.env.PUBLIC_APP_ENV = appEnv;

const child = spawn(command[0]!, command.slice(1), {
	cwd: root,
	env: process.env,
	stdio: 'inherit'
});

child.on('error', (error: NodeJS.ErrnoException) => {
	console.error(error.message);
	process.exit(1);
});

child.on('exit', (code) => {
	process.exit(code ?? 1);
});
