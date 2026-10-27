/**
 * `scripts/build-sw.ts` 在编译 Service Worker 时注入的两个常量。
 * 不要改成运行期读取：缓存名要跟构建版本绑定，产物前缀要和 SvelteKit 写进 HTML 的那个值完全一致。
 */
/** SvelteKit 的构建版本，取自 `build/client/_app/version.json`，用来给缓存命名。 */
declare const __SW_VERSION__: string;
/** 产物前缀，等于 `CDN_ASSET_PREFIX`（走 CDN 时是绝对地址，留空则为同源）。 */
declare const __SW_ASSET_BASE__: string;
