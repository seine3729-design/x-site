/**
 * 报文加密联调脚本：用浏览器同一份客户端代码（#lib/crypto/api-client.ts），经 BFF 透传调用 Go 后端。
 *
 * 用法（仓库根目录，./dev.sh up 之后）：
 *   ./dev.sh run apps bun scripts/e2e-encryption.ts http://127.0.0.1:3001
 *
 * 检查：公钥可取、加密 GET 拿到明文 JSON、网络上是密文、条件请求 304、只透传 /api/v1/*、明文请求被拒绝（后端强制加密时）。
 */
import { ENC_CONTENT_TYPE, createSecureClient, type FetchLike } from '../src/lib/crypto/api-client.ts';

const base = process.argv[2] ?? 'http://127.0.0.1:3001';
const wire: { url: string; status: number; contentType: string | null; encType: string | null; bodyPrefix: string }[] = [];

const recordingFetch: FetchLike = async (input, init) => {
	const response = await fetch(input, init);
	const clone = response.clone();
	const bytes = new Uint8Array(await clone.arrayBuffer());
	wire.push({
		url: String(input),
		status: response.status,
		contentType: response.headers.get('content-type'),
		encType: response.headers.get('x-enc-type'),
		bodyPrefix: new TextDecoder().decode(bytes.subarray(0, 24))
	});
	return response;
};

function check(name: string, ok: boolean, detail = '') {
	console.log(`${ok ? '✓' : '✗'} ${name}${detail ? ` — ${detail}` : ''}`);
	if (!ok) process.exitCode = 1;
}

const client = createSecureClient({ baseUrl: base, fetch: recordingFetch });

const config = await client.json<{ version: number }>('/api/v1/site-config');
const last = wire.at(-1)!;
check('加密 GET /api/v1/site-config 解出站点配置', typeof config.version === 'number', `version=${config.version}`);
check('网络上的响应是密文', last.contentType === ENC_CONTENT_TYPE && !last.bodyPrefix.includes('"code"'), `content-type=${last.contentType}`);
check('X-Enc-Type 带回原始类型', last.encType?.startsWith('application/json') ?? false, String(last.encType));

const etag = (await client.secureFetch('/api/v1/site-config')).headers.get('etag');
const conditional = await client.secureFetch('/api/v1/site-config', { headers: { 'if-none-match': etag ?? '' } });
check('加密请求的条件请求返回 304', conditional.status === 304, `etag=${etag}`);

const missing = await client.secureFetch('/api/v1/not-a-route');
check('未匹配的路由仍返回 404', missing.status === 404);

// 路径规范（docs/07 第 0.5 节）：BFF 只透传 /api/v1/*，后台接口与旧的无版本路径在前台域名一律 404
for (const [method, path] of [
	['POST', '/api/admin/login'],
	['GET', '/api/admin/me'],
	['GET', '/api/site-config']
] as const) {
	const res = await fetch(`${base}${path}`, { method, headers: { 'content-type': 'application/json' }, body: method === 'POST' ? '{}' : undefined });
	const body = (await res.json().catch(() => null)) as { code?: string } | null;
	check(`前台域名不转发 ${method} ${path}`, res.status === 404 && body?.code === 'NOT_FOUND', `${res.status} ${body?.code ?? ''}`);
}

const raw = await fetch(`${base}/api/v1/site-config`);
const leaked = ['x-service-token', 'x-forwarded-for', 'x-bff-proxy'].filter((h) => raw.headers.has(h));
check('BFF 不把内部请求头回传给浏览器', leaked.length === 0, leaked.join(', '));

const plain = await fetch(`${base}/api/v1/site-config`);
const plainBody = (await plain.json().catch(() => null)) as { code?: string } | null;
console.log(`  明文请求：${plain.status} ${plainBody?.code ?? ''}（后端强制加密时应为 400 ENC_REQUIRED）`);
