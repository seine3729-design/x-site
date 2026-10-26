<script lang="ts">
	import { page } from '$app/state';
	import DramaGrid from '#lib/drama/DramaGrid.svelte';
	import Poster from '#lib/drama/Poster.svelte';
	import TopicCover from '#lib/home/TopicCover.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import { titleHue } from '#lib/media/image.ts';
	import JsonLd from '#lib/seo/JsonLd.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import SectionHeader from '#lib/ui/SectionHeader.svelte';

	let { data } = $props();
	const i18n = getI18n();
	const topic = $derived(data.topic);
	const hue = $derived(titleHue(topic.title));
	const covers = $derived(topic.dramas.map((d) => d.cover_portrait).filter(Boolean).slice(0, 3));
	// FAQ 输出 FAQPage 结构化数据（docs/04 B.4）
	const faqLd = $derived({
		'@context': 'https://schema.org',
		'@type': 'FAQPage',
		mainEntity: topic.faq.map((item) => ({
			'@type': 'Question',
			name: item.question,
			acceptedAnswer: { '@type': 'Answer', text: item.answer }
		}))
	});
</script>

<svelte:head>
	<title>{i18n.t('seo.topicTitle', { title: topic.title, site: page.data.site.name })}</title>
	<meta name="description" content={topic.summary || topic.title} />
</svelte:head>
{#if topic.faq.length > 0}
	<JsonLd data={faqLd} />
{/if}

<div class="relative isolate overflow-hidden">
	<!-- 头图：专题里前几部剧的封面拼贴后重度模糊压暗，作氛围底；没有封面时退回按标题生成的渐变 -->
	{#if covers.length > 0}
		<div class="absolute inset-0 -z-10 scale-110 opacity-40 blur-2xl" aria-hidden="true">
			<TopicCover title={topic.title} {covers} />
		</div>
		<div class="absolute inset-0 -z-10 bg-gradient-to-b from-canvas/40 via-canvas/70 to-canvas" aria-hidden="true"></div>
	{:else}
		<div
			class="absolute inset-0 -z-10"
			aria-hidden="true"
			style:background="radial-gradient(70% 140% at 85% 0%, hsl({hue} 70% 40% / 0.45), transparent 60%)"
		></div>
	{/if}
	<div class="page-x pt-6 pb-8 lg:pt-12 lg:pb-12">
		<a href={i18n.path('/topics')} class="mb-3 inline-flex items-center gap-1 text-[13px] text-fg-subtle hover:text-fg">
			<Icon name="chevron-left" size={15} class="rtl:rotate-180" />{i18n.t('topic.title')}
		</a>
		<div class="flex items-end gap-5">
			<!-- 首部剧的封面作专题「封面」，电脑端才显示 -->
			{#if covers[0]}
				<span class="hidden w-28 shrink-0 overflow-hidden rounded-2xl shadow-pop ring-1 ring-white/15 lg:block">
					<Poster cover={covers[0]} title={topic.title} priority />
				</span>
			{/if}
			<div class="min-w-0">
				<h1 class="text-3xl font-extrabold tracking-tight text-balance lg:text-4xl">{topic.title}</h1>
				{#if topic.summary}<p class="mt-3 max-w-3xl text-[15px] leading-7 text-fg-muted">{topic.summary}</p>{/if}
				<p class="mt-2.5 flex items-center gap-1.5 text-[13px] text-fg-subtle">
					<Icon name="layers" size={14} />{i18n.t('topic.count', { n: topic.dramas.length })}
				</p>
			</div>
		</div>
	</div>
</div>

<div class="page-x flex flex-col gap-12">
	{#if topic.dramas.length === 0}
		<EmptyState icon="layers" title={i18n.t('topic.emptyDramas')} compact />
	{:else}
		<DramaGrid cards={topic.dramas} priorityCount={6} />
	{/if}

	{#if topic.faq.length > 0}
		<section aria-labelledby="faq-title" class="max-w-3xl">
			<SectionHeader id="faq-title" title={i18n.t('topic.faq')} />
			<div class="divide-y divide-line overflow-hidden rounded-panel border border-line bg-surface">
				{#each topic.faq as item, index (index)}
					<details class="group" open={index === 0}>
						<summary class="flex cursor-pointer list-none items-center justify-between gap-3 px-5 py-4 font-semibold [&::-webkit-details-marker]:hidden">
							{item.question}
							<Icon name="chevron-down" size={18} class="shrink-0 text-fg-subtle transition-transform group-open:rotate-180" />
						</summary>
						<p class="px-5 pb-5 text-sm leading-7 whitespace-pre-line text-fg-muted">{item.answer}</p>
					</details>
				{/each}
			</div>
		</section>
	{/if}
</div>
