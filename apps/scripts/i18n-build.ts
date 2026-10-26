/**
 * 文案发布产物（docs/04 A.4.3）：校验 src/lib/i18n/messages/*.json，输出带内容哈希的不可变文件与清单。
 *
 *   ./dev.sh run apps bun scripts/i18n-build.ts [输出目录，默认 build/i18n]
 *
 * 产物：
 *   web/<locale>.<hash>.json  内容不变则文件名不变，上传后设 Cache-Control: public, max-age=31536000, immutable
 *   web/manifest.json         当前每种语言对应的文件名，短缓存（max-age=60, stale-while-revalidate=600）
 *
 * 改文案只会产生新文件名并改写清单，CDN 上旧文件不用清理缓存；上传到对象存储的步骤在 CI 中执行。
 */
import { createHash } from 'node:crypto';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { DEFAULT_LOCALE, LOCALES } from '../src/lib/i18n/locales.ts';
import { validateCatalogs, type Catalog } from '../src/lib/i18n/validate.ts';

const root = resolve(import.meta.dirname, '..');
const outDir = resolve(root, process.argv[2] ?? 'build/i18n', 'web');
const read = (locale: string) =>
	JSON.parse(readFileSync(join(root, 'src/lib/i18n/messages', `${locale}.json`), 'utf8')) as Catalog;

const catalogs = Object.fromEntries(LOCALES.map((l) => [l, read(l)]));
const problems = validateCatalogs(catalogs[DEFAULT_LOCALE]!, catalogs);
if (problems.length > 0) {
	console.error(`文案校验失败（${problems.length} 处）：\n  ${problems.join('\n  ')}`);
	process.exit(1);
}

mkdirSync(outDir, { recursive: true });
const files: Record<string, { file: string; bytes: number }> = {};
for (const locale of LOCALES) {
	const body = JSON.stringify(catalogs[locale]);
	const hash = createHash('sha256').update(body).digest('hex').slice(0, 10);
	const file = `${locale}.${hash}.json`;
	writeFileSync(join(outDir, file), body);
	files[locale] = { file, bytes: Buffer.byteLength(body) };
}

const manifest = {
	// 清单版本 = 全部文件名的摘要：任何一种语言变化，版本就变化，客户端据此判断是否要重新下载
	version: createHash('sha256')
		.update(LOCALES.map((l) => files[l]!.file).join('\n'))
		.digest('hex')
		.slice(0, 12),
	default_locale: DEFAULT_LOCALE,
	generated_at: new Date().toISOString(),
	files
};
writeFileSync(join(outDir, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
console.log(`已输出 ${LOCALES.length} 种语言到 ${outDir}（清单版本 ${manifest.version}）`);
for (const locale of LOCALES) {
	console.log(`  ${files[locale]!.file}  ${files[locale]!.bytes} B`);
}
