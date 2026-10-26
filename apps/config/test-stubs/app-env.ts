/**
 * 单元测试用的 $app/env 替身（见 vitest.config.ts）。
 *
 * vitest 不加载 SvelteKit 插件，`$app/env` 这类虚拟模块没有解析器。被测模块里只用到 dev 与 version，
 * 这里按开发环境给值；依赖 dev 分支的行为（如生产环境的 __Host- Cookie 前缀）要在构建产物上验收，不在这里断言。
 */
export const dev = true;
export const version = 'test';
