<script lang="ts">
	import { page } from '$app/state';
	import { shortDate } from '#lib/format.ts';
	import SandboxHtml from '#lib/home/SandboxHtml.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import { getSite } from '#lib/site.ts';
	import Icon from '#lib/ui/Icon.svelte';

	let { data } = $props();
	const i18n = getI18n();
	const doc = $derived(data.doc);
	const site = $derived(getSite());
	// 页脚「帮助与条款」里的其他页面做成侧栏目录：条款、隐私、帮助这类页面用户常常要互相跳
	const siblings = $derived(site.footerPages.filter((p) => p.slug !== doc.slug));
</script>

<svelte:head>
	<title>{doc.seo_title || doc.title} - {page.data.site.name}</title>
	{#if doc.seo_desc}<meta name="description" content={doc.seo_desc} />{/if}
</svelte:head>

<div class="page-x max-w-5xl! pt-5 pb-10 lg:pt-10">
	<nav class="mb-4 flex items-center gap-1 text-[13px] text-fg-subtle" aria-label={i18n.t('nav.primary')}>
		<a href={i18n.path('/')} class="hover:text-fg">{i18n.t('nav.home')}</a>
		<Icon name="chevron-right" size={13} class="rtl:rotate-180" />
		<span class="truncate text-fg-muted">{doc.title}</span>
	</nav>

	<div class="grid gap-6 lg:grid-cols-[14rem_minmax(0,1fr)] lg:gap-8">
		<!-- 侧栏目录：电脑端吸顶，手机端收到正文下面 -->
		{#if siblings.length > 0}
			<aside class="order-2 lg:order-1" aria-labelledby="page-nav-title">
				<div class="lg:sticky lg:top-[calc(var(--header-h)+1.5rem)]">
					<h2 id="page-nav-title" class="mb-2 px-1 text-xs font-semibold tracking-wide text-fg-subtle">{i18n.t('footer.help')}</h2>
					<ul class="flex flex-col gap-0.5">
						{#each siblings as item (item.slug)}
							<li>
								<a
									href={i18n.path(`/p/${encodeURIComponent(item.slug)}`)}
									class="block truncate rounded-xl px-3 py-2 text-sm text-fg-muted transition-colors hover:bg-raised hover:text-fg"
								>
									{item.title}
								</a>
							</li>
						{/each}
					</ul>
				</div>
			</aside>
		{/if}

		<article class="order-1 min-w-0 lg:order-2">
			<header class="mb-5">
				<h1 class="text-[1.75rem] leading-tight font-extrabold tracking-tight text-balance lg:text-4xl">{doc.title}</h1>
				<p class="mt-2.5 flex items-center gap-1.5 text-[13px] text-fg-subtle">
					<Icon name="clock" size={14} />
					{i18n.t('page.updated', { date: shortDate(doc.updated_at, i18n.locale) })}
				</p>
			</header>
			<!-- 正文是后台清洗过的 HTML：放进沙箱 iframe 渲染，不进本页 DOM（docs/04 F 节）。
			     外层是卡片，所以框内底色取 surface，接缝看不出来 -->
			<div class="overflow-hidden rounded-panel border border-line bg-surface p-5 lg:p-8">
				<SandboxHtml html={doc.body} title={doc.title} minHeight={240} prose surface="surface" />
			</div>
		</article>
	</div>
</div>
