<script lang="ts">
	import { page } from '$app/state';
	import { getI18n } from '#lib/i18n/context.ts';
	import EmptyState from '#lib/ui/EmptyState.svelte';

	// 离线兜底页（Service Worker 安装时预缓存，导航失败且没有缓存时显示）。
	// 按钮用普通链接整页跳转：断网时客户端路由取不到页面数据，点了只会卡住。
	const i18n = getI18n();
</script>

<svelte:head>
	<title>{i18n.t('offline.title')} - {page.data.site.name}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="page-x">
	<EmptyState icon="wifi-off" title={i18n.t('offline.title')} description={i18n.t('offline.desc')}>
		<a href={i18n.path('/')} class="btn btn-primary" data-sveltekit-reload>{i18n.t('offline.retry')}</a>
		<a href={i18n.path('/way-home')} class="btn btn-ghost" data-sveltekit-reload>{i18n.t('footer.wayHome')}</a>
	</EmptyState>
</div>
