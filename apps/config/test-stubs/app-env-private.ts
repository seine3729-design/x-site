/**
 * 单元测试用的 $app/env/private 替身（见 vitest.config.ts）。
 *
 * 全是明显的假值：单元测试不连后端，取到这些值的代码路径（真正发请求、校验内网令牌）
 * 由容器内的接口联调与构建产物验收覆盖。真实值只存在于 .env，不进仓库。
 */
export const SERVICE_INTERNAL_URL = 'http://service.invalid';
export const BFF_SERVICE_TOKEN = 'test-not-a-real-token';
export const APP_ENV = 'test';
export const CDN_ASSET_PREFIX = '';
export const IMAGE_BASE_URLS = '';
export const RATE_LIMIT_READ_PER_MIN = '600';
export const RATE_LIMIT_WRITE_PER_MIN = '60';
