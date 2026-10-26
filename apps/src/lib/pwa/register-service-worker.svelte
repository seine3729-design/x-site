<script lang="ts">
	import { dev } from '$app/env';
	import { onMount } from 'svelte';

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
		let reloading = false;

		const onMessage = (event: MessageEvent) => {
			if (event.data?.type !== 'sv-reload' || reloading) {
				return;
			}
			// 播放中不打断：新版本在下一次导航或刷新时生效
			if ([...document.querySelectorAll('video')].some((video) => !video.paused)) {
				return;
			}
			reloading = true;
			window.location.reload();
		};

		const onVisible = () => {
			if (document.visibilityState === 'visible') {
				void registration?.update();
			}
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
					return next.update();
				})
				.catch(() => undefined);
		};

		navigator.serviceWorker.addEventListener('message', onMessage);
		document.addEventListener('visibilitychange', onVisible);

		if (document.readyState === 'complete') {
			register();
		} else {
			window.addEventListener('load', register, { once: true });
		}

		return () => {
			window.removeEventListener('beforeinstallprompt', blockInstallPrompt);
			navigator.serviceWorker.removeEventListener('message', onMessage);
			document.removeEventListener('visibilitychange', onVisible);
			window.removeEventListener('load', register);
		};
	});
</script>
