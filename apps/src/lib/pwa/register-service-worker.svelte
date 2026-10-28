<script lang="ts">
	import { dev } from '$app/env';
	import { onMount } from 'svelte';

	/** 两次「查有没有新版本」之间至少隔这么久 */
	const UPDATE_CHECK_INTERVAL_MS = 30 * 60 * 1000;

	// PWA 只取 Service Worker 带来的加载速度与离线兜底，不提示安装（docs/04 D 节）：
	// 站点刻意不提供 web app manifest——浏览器判定「可安装」的前提之一就是 manifest，没有它就不会出现
	// 地址栏安装图标与安装横幅；万一某个浏览器仍触发 beforeinstallprompt，也在这里拦下不让它弹出。
	onMount(() => {
		const blockInstallPrompt = (event: Event) => event.preventDefault();
		window.addEventListener('beforeinstallprompt', blockInstallPrompt);

		if (dev || !('serviceWorker' in navigator)) {
			return () => window.removeEventListener('beforeinstallprompt', blockInstallPrompt);
		}

		let registration: ServiceWorkerRegistration | undefined;
		let lastCheck = 0;

		/*
		 * 这里刻意不处理「Service Worker 说有新版本」然后自动 location.reload()。
		 * 自动重载会把正在浏览的用户打回页面顶部、图片全部重新加载，而且一旦版本号不稳定就会
		 * 变成死循环（线上出过：同时有 3 个构建版本在跑，页面反复刷新）。
		 * 新版本由 SW 的 clients.claim() 接管后续请求，在下一次导航或用户自己刷新时生效。
		 */
		const onVisible = () => {
			if (document.visibilityState !== 'visible') {
				return;
			}
			// 移动端滑动时地址栏显隐就会触发 visibilitychange，别每次都去查更新
			const now = Date.now();
			if (now - lastCheck < UPDATE_CHECK_INTERVAL_MS) {
				return;
			}
			lastCheck = now;
			void registration?.update();
		};

		const register = () => {
			void navigator.serviceWorker
				.register('/service-worker.js', {
					scope: '/',
					type: 'module',
					updateViaCache: 'none'
				})
				.then((next) => {
					registration = next;
					lastCheck = Date.now();
					return next.update();
				})
				.catch(() => undefined);
		};

		document.addEventListener('visibilitychange', onVisible);

		if (document.readyState === 'complete') {
			register();
		} else {
			window.addEventListener('load', register, { once: true });
		}

		return () => {
			window.removeEventListener('beforeinstallprompt', blockInstallPrompt);
			document.removeEventListener('visibilitychange', onVisible);
			window.removeEventListener('load', register);
		};
	});
</script>
