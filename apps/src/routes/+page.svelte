<script lang="ts">
	import { page } from '$app/state';
	import type { HomeBlock } from '#lib/api/types.ts';
	import AdSlot from '#lib/home/AdSlot.svelte';
	import BlockRenderer from '#lib/home/BlockRenderer.svelte';
	import LazyMount from '#lib/home/LazyMount.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import Boundary from '#lib/ui/Boundary.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';

	let { data } = $props();
	const i18n = getI18n();

	// 默认首页（后台未编排区块）的标题来自文案；后台编排的区块用后台填的标题
	const titled = (block: HomeBlock): HomeBlock => {
		if (!data.fallback) {
			return block;
		}
		const title =
			block.type === 'h_scroll' ? i18n.t('home.hot') : block.type === 'poster_grid' ? i18n.t('home.latest') : block.title;
		return { ...block, title };
	};

	// 按设备过滤（电脑与手机共享一份缓存，后端原样下发 device）：用 CSS 隐藏，避免 SSR 与水合不一致
	const deviceClass = (block: HomeBlock) =>
		block.device === 'mobile' ? 'lg:hidden' : block.device === 'desktop' ? 'max-lg:hidden' : '';

	const description = $derived(page.data.site.slogan || i18n.t('home.description'));
	/** 首屏直接渲染的区块数，其余按可视区挂载（docs/04 B.2） */
	const EAGER_BLOCKS = 3;
</script>

<svelte:head>
	<title>{page.data.site.slogan ? `${page.data.site.name} - ${page.data.site.slogan}` : page.data.site.name}</title>
	<meta name="description" content={description} />
	<meta property="og:title" content={page.data.site.name} />
	<meta property="og:description" content={description} />
	<meta property="og:type" content="website" />
</svelte:head>

<h1 class="sr-only">{page.data.site.name}</h1>

<!-- 首页顶部 / 底部广告位（docs/04 C.5）。home_mid 由后台的「广告位」区块编排，不在这里写死 -->
<div class="page-x"><AdSlot slot="home_top" class="pt-3" /></div>

{#if data.blocks.length === 0}
	<div class="page-x">
		<EmptyState icon="clapperboard" title={i18n.t('home.emptyTitle')} description={i18n.t('home.emptyDesc')}>
			<a href={i18n.path('/explore')} class="btn btn-primary">{i18n.t('home.goExplore')}</a>
		</EmptyState>
	</div>
{:else}
	<div class="page-x flex flex-col gap-4 pt-0 md:gap-3">
		{#each data.blocks as raw, index (raw.id)}
			{@const block = titled(raw)}
			<div class={deviceClass(block)}>
				<!-- 单个区块渲染出错只换成一条可重试的提示，不拖垮整页（docs/04 B.1） -->
				<Boundary label={block.type}>
					{#if index < EAGER_BLOCKS}
						<BlockRenderer {block} {index} />
					{:else}
						<LazyMount minHeight={block.type === 'poster_grid' ? '32rem' : '16rem'}>
							<BlockRenderer {block} {index} />
						</LazyMount>
					{/if}
				</Boundary>
			</div>
		{/each}
	</div>
{/if}

<div class="page-x"><AdSlot slot="home_bottom" class="py-4" /></div>
