import { callContext } from '#lib/server/context.ts';
import type { RequestHandler } from './$types';

// /api/v1/* 之外的 /api/* 一律 404（docs/07 第 0.5 节）：BFF 只透传前台接口，
// 后台接口 /api/admin/* 只在后台域名提供，不能经前台域名转发到后端。返回与后端一致的 JSON 错误包。
export const fallback: RequestHandler = (event) =>
	Response.json(
		{ code: 'NOT_FOUND', message: '接口不存在', request_id: callContext(event).requestId },
		{ status: 404, headers: { 'cache-control': 'no-store' } }
	);
