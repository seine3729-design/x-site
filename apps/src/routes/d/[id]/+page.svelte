<script lang="ts">
	import { page } from '$app/state';
	import { PUBLIC_SITE_URL } from '$app/env/public';
	import { onMount } from 'svelte';
	import AdSlot from '#lib/home/AdSlot.svelte';
	import Badge from '#lib/drama/Badge.svelte';
	import DramaActions from '#lib/drama/DramaActions.svelte';
	import DramaGrid from '#lib/drama/DramaGrid.svelte';
	import EpisodeGrid from '#lib/drama/EpisodeGrid.svelte';
	import { episodesLabel, firstPlayable, playPath } from '#lib/drama/labels.ts';
	import Poster from '#lib/drama/Poster.svelte';
	import { compactNumber, score } from '#lib/format.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import { localHistory, type LocalHistory } from '#lib/local/store.ts';
	import { imageSources } from '#lib/media/image.ts';
	import JsonLd from '#lib/seo/JsonLd.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import SectionHeader from '#lib/ui/SectionHeader.svelte';

	let { data } = $props();
	const i18n = getI18n();
	const drama = $derived(data.drama);
	const rating = $derived(score(drama.score));
	const first = $derived(firstPlayable(drama.episodes));
	const channels = $derived(drama.terms.filter((t) => t.is_channel));
	const otherTerms = $derived(drama.terms.filter((t) => !t.is_channel));
	const tags = $derived(drama.tags.filter((t) => !t.is_sensitive));

	// 续看：本机记录（游客数据或登录用户的本机缓存）里有这部剧就把主按钮换成「续看第 N 集」
	let resume = $state<LocalHistory | null>(null);
	onMount(() => {
		void localHistory.get(drama.id).then((entry) => (resume = entry ?? null));
	});
	const resumeEpisode = $derived(
		resume ? drama.episodes.find((e) => e.number === resume!.episode_number && e.playable) : undefined
	);

	let expanded = $state(false);
	const longSummary = $derived(drama.summary.length > 90);

	const origin = $derived(PUBLIC_SITE_URL || page.url.origin);
	const canonical = $derived(origin + i18n.path(`/d/${drama.id}`));
	// 分享卡片：有可直接访问的封面就用封面，否则用分享卡片接口（S3-8）
	const ogImage = $derived(
		imageSources(drama.cover_landscape || drama.cover_portrait, page.data.site.imageBases)[0] ??
			`${origin}/api/v1/og/drama/${drama.id}`
	);
	const title = $derived(drama.seo_title || i18n.t('seo.dramaTitle', { title: drama.title, site: page.data.site.name }));
	const description = $derived(drama.seo_desc || drama.summary.slice(0, 120) || i18n.t('seo.dramaDesc', { title: drama.title }));
	const structured = $derived({
		'@context': 'https://schema.org',
		'@type': 'TVSeries',
		name: drama.title,
		alternateName: drama.aliases.length > 0 ? drama.aliases : undefined,
		description: drama.summary || undefined,
		url: canonical,
		numberOfEpisodes: drama.episode_count,
		datePublished: drama.publish_at ?? undefined,
		genre: drama.terms.map((t) => t.name),
		keywords: tags.map((t) => t.name).join(', ') || undefined,
		aggregateRating:
			rating !== null ? { '@type': 'AggregateRating', ratingValue: rating, bestRating: 10, ratingCount: Math.max(1, drama.play_count) } : undefined,
		episode: drama.episodes
			.filter((e) => e.playable)
			.slice(0, 100)
			.map((e) => ({
				'@type': 'TVEpisode',
				episodeNumber: e.number,
				name: e.title || i18n.t('episode.label', { n: e.number }),
				url: origin + i18n.path(playPath(drama.id, e.number))
			}))
	});
</script>

<svelte:head>
	<title>{title}</title>
	<meta name="description" content={description} />
	<link rel="canonical" href={canonical} />
	<meta property="og:type" content="video.tv_show" />
	<meta property="og:title" content={drama.title} />
	<meta property="og:description" content={description} />
	<meta property="og:url" content={canonical} />
	<meta property="og:image" content={ogImage} />
	<meta name="twitter:card" content="summary_large_image" />
	{#if drama.tags.some((t) => t.is_sensitive)}
		<meta name="robots" content="noindex" />
	{/if}
</svelte:head>
<JsonLd data={structured} />

<article>
	<!-- 顶部：模糊封面作氛围背景，向下渐隐到页面底色 -->
	<div class="relative isolate overflow-hidden">
		<div class="absolute inset-0 -z-10" aria-hidden="true">
			<Poster cover={drama.cover_landscape || drama.cover_portrait} title={drama.title} class="absolute! inset-0 size-full scale-110 opacity-45 blur-3xl" />
			<div class="absolute inset-0 bg-gradient-to-b from-canvas/40 via-canvas/75 to-canvas"></div>
		</div>

		<div class="page-x flex flex-col gap-6 pt-5 pb-8 md:flex-row md:gap-10 md:pt-10 lg:pb-12">
			<div class="mx-auto w-44 shrink-0 sm:w-52 md:mx-0 md:w-60 lg:w-64">
				<div class="overflow-hidden rounded-card shadow-pop ring-1 ring-white/10">
					<Poster cover={drama.cover_portrait} title={drama.title} priority />
				</div>
			</div>

			<div class="flex min-w-0 flex-1 flex-col gap-4 md:py-2">
				<div class="text-center md:text-start">
					<Badge value={drama.badge} size="md" class="mb-3" />
					<h1 class="text-2xl leading-tight font-extrabold tracking-tight text-balance md:text-4xl">{drama.title}</h1>
					{#if drama.aliases.length > 0}
						<p class="mt-1.5 text-sm text-fg-subtle">{drama.aliases.join(' / ')}</p>
					{/if}
				</div>

				<dl class="flex flex-wrap items-center justify-center gap-x-5 gap-y-2 text-sm text-fg-muted md:justify-start">
					{#if rating}
						<div class="flex items-center gap-1">
							<dt class="sr-only">{i18n.t('drama.score')}</dt>
							<Icon name="star" size={16} filled strokeWidth={0} class="text-vip-fg" />
							<dd class="text-lg font-extrabold text-vip-fg tabular">{rating}</dd>
						</div>
					{/if}
					<div>
						<dt class="sr-only">{i18n.t('drama.episodes')}</dt>
						<dd>{episodesLabel(i18n, drama)}</dd>
					</div>
					{#if drama.year}
						<div>
							<dt class="sr-only">{i18n.t('drama.year')}</dt>
							<dd>{drama.year}</dd>
						</div>
					{/if}
					{#if drama.play_count > 0}
						<div class="flex items-center gap-1">
							<dt class="sr-only">{i18n.t('drama.playCount')}</dt>
							<Icon name="flame" size={15} class="text-brand-fg" />
							<dd>{i18n.t('drama.plays', { n: compactNumber(drama.play_count, i18n.locale) })}</dd>
						</div>
					{/if}
				</dl>

				{#if channels.length + otherTerms.length + tags.length > 0}
					<ul class="flex flex-wrap justify-center gap-2 md:justify-start">
						{#each channels as term (term.id)}
							<li><a href={i18n.path(`/c/${encodeURIComponent(term.slug)}`)} class="chip">{term.name}</a></li>
						{/each}
						{#each otherTerms as term (term.id)}
							<li>
								<a
									href={i18n.path(`/explore?terms=${encodeURIComponent(`${term.taxonomy_slug}:${term.slug}`)}`)}
									class="chip">{term.name}</a
								>
							</li>
						{/each}
						{#each tags as tag (tag.id)}
							<li><a href={i18n.path(`/tag/${encodeURIComponent(tag.slug)}`)} class="chip">#{tag.name}</a></li>
						{/each}
					</ul>
				{/if}

				{#if drama.summary}
					<div class="max-w-3xl">
						<p
							id="drama-summary"
							class="text-[15px] leading-7 whitespace-pre-line text-fg-muted {expanded ? '' : 'line-clamp-3'}"
						>
							{drama.summary}
						</p>
						{#if longSummary}
							<button
								type="button"
								class="mt-1 inline-flex items-center gap-0.5 text-sm font-semibold text-brand-fg"
								aria-expanded={expanded}
								aria-controls="drama-summary"
								onclick={() => (expanded = !expanded)}
							>
								{i18n.t(expanded ? 'common.collapse' : 'common.expand')}
								<Icon name="chevron-down" size={16} class={expanded ? 'rotate-180' : ''} />
							</button>
						{/if}
					</div>
				{/if}

				<div class="mt-1 flex flex-col items-stretch gap-3 sm:flex-row sm:items-center">
					{#if resumeEpisode}
						<a href={i18n.path(playPath(drama.id, resumeEpisode.number))} class="btn btn-lg btn-primary min-w-48">
							<Icon name="play" size={18} filled strokeWidth={0} />
							{i18n.t('drama.continue', { n: resumeEpisode.number })}
						</a>
					{:else if first}
						<a href={i18n.path(playPath(drama.id, first.number))} class="btn btn-lg btn-primary min-w-48">
							<Icon name="play" size={18} filled strokeWidth={0} />
							{i18n.t('drama.playNow')}
						</a>
					{:else}
						<span class="btn btn-lg btn-ghost pointer-events-none min-w-48 opacity-70">{i18n.t('drama.comingSoon')}</span>
					{/if}
					<div class="flex justify-center sm:justify-start">
						<DramaActions {drama} />
					</div>
				</div>
			</div>
		</div>
	</div>

	<div class="page-x flex flex-col gap-10 md:gap-12">
		<!-- 详情页横幅广告位（docs/04 C.5）：没有素材时整块不渲染，不占位 -->
		<AdSlot slot="detail_banner" class="-mb-4 md:-mb-6" />

		{#if drama.episodes.length > 0}
			<section aria-labelledby="episodes-title">
				<SectionHeader id="episodes-title" title={i18n.t('drama.episodes')} subtitle={episodesLabel(i18n, drama)} />
				<EpisodeGrid dramaId={drama.id} episodes={drama.episodes} />
			</section>
		{/if}

		{#if drama.related.length > 0}
			<section aria-labelledby="related-title">
				<SectionHeader id="related-title" title={i18n.t('drama.related')} />
				<DramaGrid cards={drama.related} />
			</section>
		{/if}
	</div>
</article>
