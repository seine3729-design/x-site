<script lang="ts">
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import { getSite } from '#lib/site.ts';
	import { toasts } from '#lib/stores/toast.svelte.ts';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Icon from '#lib/ui/Icon.svelte';

	const i18n = getI18n();
	const site = $derived(getSite());
	const addresses = $derived(site.addresses);

	type Probe = 'checking' | 'fast' | 'normal' | 'down';
	type LabelKey = 'wayHome.latest' | 'wayHome.permanent' | 'wayHome.backup';
	let probes = $state<Record<string, Probe>>({});

	/** 超时信号：AbortSignal.timeout 在 Safari 15 没有，退回手动计时（docs/04 A.6） */
	function timeoutSignal(ms: number): { signal: AbortSignal; done: () => void } {
		if (typeof AbortSignal.timeout === 'function') {
			return { signal: AbortSignal.timeout(ms), done: () => {} };
		}
		const controller = new AbortController();
		const timer = setTimeout(() => controller.abort(), ms);
		return { signal: controller.signal, done: () => clearTimeout(timer) };
	}

	// 逐条实时探活（docs/04 B.14）：跨域只能拿到「通 / 不通」与耗时，用 no-cors 请求，不读响应内容
	async function probe(url: string): Promise<Probe> {
		const started = performance.now();
		const { signal, done } = timeoutSignal(6000);
		try {
			await fetch(url, { mode: 'no-cors', cache: 'no-store', signal });
			return performance.now() - started < 800 ? 'fast' : 'normal';
		} catch {
			return 'down';
		} finally {
			done();
		}
	}

	/**
	 * 同一个地址只出一行，分类标签合起来显示。
	 * 后台完全可以把「最新地址」和「永久地址」填成同一个域名（本地库里就是），
	 * 以前按 url 做 keyed each 的 key，重复 key 会让 Svelte 直接抛 each_key_duplicate、整页变成 500。
	 */
	const list = $derived.by(() => {
		if (!addresses) {
			return [];
		}
		const raw: { key: LabelKey; url: string }[] = [];
		if (addresses.latest_url) raw.push({ key: 'wayHome.latest', url: addresses.latest_url });
		if (addresses.permanent_url) raw.push({ key: 'wayHome.permanent', url: addresses.permanent_url });
		for (const url of addresses.backup_urls ?? []) raw.push({ key: 'wayHome.backup', url });

		const merged: { url: string; keys: LabelKey[] }[] = [];
		for (const item of raw) {
			const url = item.url.trim();
			if (!url) {
				continue;
			}
			const found = merged.find((m) => m.url === url);
			if (!found) {
				merged.push({ url, keys: [item.key] });
			} else if (!found.keys.includes(item.key)) {
				found.keys.push(item.key);
			}
		}
		return merged;
	});

	onMount(() => {
		for (const item of list) {
			probes[item.url] = 'checking';
			void probe(item.url).then((result) => (probes[item.url] = result));
		}
	});

	async function copy(url: string) {
		try {
			await navigator.clipboard.writeText(url);
			toasts.show(i18n.t('share.copied'), { tone: 'success' });
		} catch {
			toasts.show(i18n.t('share.failed'), { tone: 'error' });
		}
	}

	const PROBE_STYLE: Record<Probe, string> = {
		checking: 'bg-raised text-fg-subtle',
		fast: 'bg-ok/15 text-ok',
		normal: 'bg-warn/15 text-warn',
		down: 'bg-danger/15 text-danger'
	};
	const PROBE_DOT: Record<Probe, string> = {
		checking: 'bg-fg-subtle animate-pulse',
		fast: 'bg-ok',
		normal: 'bg-warn',
		down: 'bg-danger'
	};
	const PROBE_KEY = { checking: 'wayHome.checking', fast: 'wayHome.fast', normal: 'wayHome.normal', down: 'wayHome.down' } as const;
	const links = $derived(
		addresses
			? [
					{ url: addresses.publish_page, key: 'wayHome.publish' as const, icon: 'globe' as const },
					{ url: addresses.support_url, key: 'wayHome.support' as const, icon: 'message-circle' as const },
					{ url: addresses.telegram, key: 'wayHome.telegram' as const, icon: 'send' as const }
				].filter((l) => /^https?:\/\//i.test(l.url))
			: []
	);
	const host = (url: string) => url.replace(/^https?:\/\//i, '').replace(/\/$/, '');
</script>

<svelte:head>
	<title>{i18n.t('footer.wayHome')} - {page.data.site.name}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="page-x max-w-3xl! pt-6 pb-10 lg:pt-12">
	<!-- 头部：这页是用户被墙之后才来的，所以标题下第一句就是「怎么用」 -->
	<div class="relative overflow-hidden rounded-panel border border-line bg-surface px-5 py-7 text-center sm:px-8 sm:py-9">
		<div
			class="pointer-events-none absolute inset-x-0 -top-24 h-48 opacity-70 blur-3xl"
			style:background="radial-gradient(45% 100% at 50% 100%, var(--brand-soft), transparent 70%)"
			aria-hidden="true"
		></div>
		<span class="relative mx-auto mb-4 grid size-14 place-items-center rounded-2xl bg-brand text-on-brand shadow-pop">
			<Icon name="map-pin" size={28} />
		</span>
		<h1 class="relative text-[1.375rem] font-extrabold tracking-tight sm:text-3xl">{i18n.t('footer.wayHome')}</h1>
		<p class="relative mx-auto mt-2.5 max-w-xl text-[13px] leading-6 text-fg-muted sm:text-sm">
			{addresses?.notice || i18n.t('wayHome.tip')}
		</p>
	</div>

	{#if list.length === 0 && links.length === 0}
		<div class="mt-4">
			<EmptyState icon="link" title={i18n.t('wayHome.empty')} compact />
		</div>
	{:else}
		{#if list.length > 0}
			<ul class="mt-4 flex flex-col gap-2.5 sm:gap-3">
				{#each list as item, index (item.url)}
					{@const status = probes[item.url] ?? 'checking'}
					<!-- 第一条是运营填的「最新地址」，加品牌描边当主推，手机上一眼就知道先点哪个 -->
					<li
						class="flex flex-wrap items-center gap-x-3 gap-y-3 rounded-panel border p-3.5 transition-colors sm:flex-nowrap sm:p-4 {index === 0
							? 'border-brand-fg/45 bg-brand-soft/35'
							: 'border-line bg-surface hover:border-line-strong'}"
					>
						<div class="min-w-0 flex-1 basis-full sm:basis-auto">
							<p class="flex flex-wrap items-center gap-1.5">
								{#each item.keys as key (key)}
									<span class="rounded-md bg-raised px-1.5 py-0.5 text-[11px] leading-4 font-semibold text-fg-muted">{i18n.t(key)}</span>
								{/each}
								<span class="flex items-center gap-1 text-[11px] leading-4 font-semibold {PROBE_STYLE[status]} rounded-md px-1.5 py-0.5">
									<span class="size-1.5 rounded-full {PROBE_DOT[status]}" aria-hidden="true"></span>
									<span aria-live="polite">{i18n.t(PROBE_KEY[status])}</span>
								</span>
							</p>
							<a
								href={item.url}
								target="_blank"
								rel="noopener noreferrer"
								class="mt-1.5 block truncate text-base font-bold tracking-tight tabular hover:text-brand-fg sm:text-lg"
							>
								{host(item.url)}
							</a>
						</div>
						<div class="flex shrink-0 items-center gap-2">
							<button type="button" class="btn btn-sm btn-ghost gap-1.5" onclick={() => copy(item.url)}>
								<Icon name="copy" size={15} />
								{i18n.t('common.copy')}
							</button>
							<a
								href={item.url}
								target="_blank"
								rel="noopener noreferrer"
								class="btn btn-sm gap-1.5 {index === 0 ? 'btn-primary' : 'btn-ghost'}"
							>
								{i18n.t('wayHome.open')}
								<Icon name="arrow-right" size={15} class="rtl:rotate-180" />
							</a>
						</div>
					</li>
				{/each}
			</ul>
		{/if}

		{#if links.length > 0 || (addresses?.socials.length ?? 0) > 0}
			<h2 class="mt-7 mb-3 px-1 text-sm font-bold text-fg-muted">{i18n.t('wayHome.contact')}</h2>
			<div class="grid grid-cols-2 gap-2.5 sm:grid-cols-3 sm:gap-3">
				{#each links as link (link.key)}
					<a
						href={link.url}
						target="_blank"
						rel="noopener noreferrer"
						class="flex min-h-12 items-center gap-2.5 rounded-panel border border-line bg-surface px-3.5 py-3 text-[13px] font-semibold transition-colors hover:border-line-strong sm:text-sm"
					>
						<Icon name={link.icon} size={18} class="shrink-0 text-brand-fg" />
						<span class="min-w-0 flex-1 truncate">{i18n.t(link.key)}</span>
						<Icon name="external-link" size={13} class="shrink-0 text-fg-subtle" />
					</a>
				{/each}
				{#each addresses?.socials ?? [] as social (social.url)}
					{#if /^https?:\/\//i.test(social.url)}
						<a
							href={social.url}
							target="_blank"
							rel="noopener noreferrer"
							class="flex min-h-12 items-center gap-2.5 rounded-panel border border-line bg-surface px-3.5 py-3 text-[13px] font-semibold transition-colors hover:border-line-strong sm:text-sm"
						>
							<Icon name="link" size={18} class="shrink-0 text-brand-fg" />
							<span class="min-w-0 flex-1 truncate">{social.name}</span>
							<Icon name="external-link" size={13} class="shrink-0 text-fg-subtle" />
						</a>
					{/if}
				{/each}
			</div>
		{/if}
	{/if}

	<p class="mt-7 flex items-start justify-center gap-2 rounded-panel bg-raised/50 p-4 text-xs leading-5 text-fg-subtle">
		<Icon name="bookmark" size={15} class="mt-px shrink-0" />
		<span class="text-start">{i18n.t('wayHome.bookmark')}</span>
	</p>
</div>
