<script lang="ts">
	import { page } from '$app/state';
	import { getI18n } from '#lib/i18n/context.ts';
	import EmptyState from '#lib/ui/EmptyState.svelte';

	const i18n = getI18n();
	// 404「内容不见了」、429「操作太频繁」、5xx「服务开小差」，其余「页面打不开」（docs/04 B.1）
	const view = $derived.by(() => {
		const status = page.status;
		if (status === 404) {
			return { icon: 'film' as const, title: i18n.t('errorPage.notFound'), desc: i18n.t('error.NOT_FOUND') };
		}
		if (status === 429) {
			return { icon: 'clock' as const, title: i18n.t('errorPage.tooMany'), desc: i18n.t('error.RATE_LIMITED') };
		}
		if (status >= 500) {
			return { icon: 'wifi-off' as const, title: i18n.t('errorPage.server'), desc: i18n.t('error.INTERNAL') };
		}
		return { icon: 'triangle-alert' as const, title: i18n.t('errorPage.generic'), desc: i18n.t('errorPage.genericDesc') };
	});
</script>

<svelte:head>
	<title>{view.title} - {page.data.site?.name ?? ''}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="page-x">
	<EmptyState icon={view.icon} title={view.title} description={view.desc} heading="h1">
		<a href={i18n.path('/explore')} class="btn btn-primary">{i18n.t('home.goExplore')}</a>
		<a href={i18n.path('/')} class="btn btn-ghost">{i18n.t('errorPage.home')}</a>
	</EmptyState>
	<p class="pb-10 text-center text-xs text-fg-subtle tabular">{page.status}</p>
</div>
