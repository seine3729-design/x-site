<script lang="ts">
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import type { VipPlan, VipPlansResponse, VipStatus } from '#lib/api/types.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import { session } from '#lib/stores/session.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';

	type VipChannel = VipPlan['channels'][number];
	type MockOrder = { order_no: string; status: string; expire_at: string };
	type MockAction = { type: 'mock'; complete_endpoint: string };

	const PLAN_NAME_KEYS = {
		day: 'vip.plan.day',
		week: 'vip.plan.week',
		month: 'vip.plan.month',
		quarter: 'vip.plan.quarter',
		year: 'vip.plan.year'
	} as const;
	const CHANNEL_LABEL_KEYS = {
		alipay: 'vip.channel.alipay',
		wechat: 'vip.channel.wechat',
		usdt: 'vip.channel.usdt',
		iap_apple: 'vip.channel.iap_apple',
		iap_google: 'vip.channel.iap_google'
	} as const;
	const PERKS = [
		{ icon: 'film', key: 'vip.perkAll' },
		{ icon: 'zap', key: 'vip.perkEarly' },
		{ icon: 'gauge', key: 'vip.perkHd' },
		{ icon: 'shield-check', key: 'vip.perkNoAds' }
	] as const;

	const i18n = getI18n();
	let plans = $state<VipPlan[] | null>(null);
	let client = $state<VipPlansResponse['client']>('web_pc');
	let mockEnabled = $state(false);
	let loadFailed = $state(false);
	let vipStatus = $state<VipStatus | null>(null);
	let vipStatusPending = $state(false);
	let vipStatusFailed = $state(false);
	let selectedPlan = $state<VipPlan | null>(null);
	let selectedChannel = $state<VipChannel | ''>('');
	let mockOrder = $state<MockOrder | null>(null);
	let mockAction = $state<MockAction | null>(null);
	let paymentPending = $state(false);
	let paymentFailed = $state(false);
	let vipStatusRequest = 0;

	const returnPath = $derived(safeInternalPath(page.url.searchParams.get('next')));

	function safeInternalPath(value: string | null) {
		if (!value || !value.startsWith('/') || value.startsWith('//') || value.includes('\\')) return null;
		return value;
	}

	function signInHref() {
		return i18n.path(`/login?next=${encodeURIComponent(`/vip${page.url.search}`)}`);
	}

	async function loadPlans() {
		plans = null;
		loadFailed = false;
		try {
			const { api } = await import('#lib/api/client.ts');
			const data = await api.get<VipPlansResponse>('/api/v1/vip/plans');
			client = data.client;
			mockEnabled = data.mock_enabled;
			plans = data.plans ?? [];
		} catch {
			loadFailed = true;
			plans = [];
		}
	}

	async function loadVipStatus() {
		const request = ++vipStatusRequest;
		vipStatusPending = true;
		vipStatusFailed = false;
		try {
			const { api } = await import('#lib/api/client.ts');
			const status = await api.get<VipStatus>('/api/v1/me/vip');
			if (request === vipStatusRequest) vipStatus = status;
		} catch {
			if (request === vipStatusRequest) vipStatusFailed = true;
		} finally {
			if (request === vipStatusRequest) vipStatusPending = false;
		}
	}

	onMount(() => {
		void loadPlans();
	});

	// 个人会员状态只在浏览器确认登录后读取，不能混入可缓存的公开页面数据。
	$effect(() => {
		const isUser = session.status === 'user';
		if (!isUser) {
			vipStatusRequest += 1;
			vipStatus = null;
			vipStatusPending = false;
			vipStatusFailed = false;
			return;
		}
		void loadVipStatus();
	});

	function planName(plan: VipPlan) {
		return i18n.t(PLAN_NAME_KEYS[plan.code]);
	}

	function planBadge(plan: VipPlan) {
		if (plan.badge_key === 'vip.recommended' || plan.recommended) return i18n.t('vip.recommended');
		return '';
	}

	function planDays(plan: VipPlan) {
		return i18n.t('vip.durationDays', { n: plan.duration_days });
	}

	function cny(fen: number) {
		return new Intl.NumberFormat(i18n.locale, { style: 'currency', currency: 'CNY', maximumFractionDigits: 2 }).format(fen / 100);
	}

	function channelLabel(channel: VipChannel) {
		return i18n.t(CHANNEL_LABEL_KEYS[channel]);
	}

	function planSelectionLocked() {
		return paymentPending || (mockAction !== null && mockOrder?.status !== 'paid');
	}

	function selectPlan(plan: VipPlan) {
		if (!mockEnabled || planSelectionLocked()) return;
		selectedPlan = plan;
		selectedChannel = plan.channels[0] ?? '';
		mockOrder = null;
		mockAction = null;
		paymentFailed = false;
	}

	function membershipTitle() {
		if (vipStatus?.is_vip) return i18n.t('vip.memberActive');
		if (vipStatus && !vipStatusPending && !vipStatusFailed) return i18n.t('vip.memberInactive');
		return i18n.t('vip.bannerTitle');
	}

	function membershipDescription() {
		if (vipStatusPending) return i18n.t('vip.memberStatusLoading');
		if (vipStatusFailed) return i18n.t('vip.memberStatusUnavailable');
		if (vipStatus?.is_vip && vipStatus.expire_at) {
			const date = new Date(vipStatus.expire_at);
			if (!Number.isNaN(date.getTime())) {
				return i18n.t('vip.memberExpiresAt', { date: new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium' }).format(date) });
			}
		}
		return i18n.t('vip.bannerDesc');
	}

	async function createMockOrder() {
		if (!selectedPlan || !selectedChannel) return;
		if (session.status !== 'user') {
			window.location.assign(signInHref());
			return;
		}
		paymentPending = true;
		paymentFailed = false;
		try {
			const { api } = await import('#lib/api/client.ts');
			const result = await api.post<{ order: MockOrder; pay_action: MockAction }>('/api/v1/pay/orders', {
				product: { type: 'vip_plan', id: selectedPlan.id },
				channel: selectedChannel,
				client,
				return_path: returnPath ?? '/vip'
			}, { headers: { 'Idempotency-Key': crypto.randomUUID() } });
			mockOrder = result.order;
			mockAction = result.pay_action;
		} catch {
			paymentFailed = true;
		} finally {
			paymentPending = false;
		}
	}

	async function completeMockPayment() {
		if (!mockAction) return;
		paymentPending = true;
		paymentFailed = false;
		try {
			const { api } = await import('#lib/api/client.ts');
			mockOrder = await api.post<MockOrder>(mockAction.complete_endpoint);
			await session.refresh(true);
			await loadVipStatus();
		} catch {
			paymentFailed = true;
		} finally {
			paymentPending = false;
		}
	}
</script>

<svelte:head>
	<title>{i18n.t('vip.title')} - {page.data.site.name}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="vip-page relative isolate overflow-hidden">
	<div class="vip-hero-glow absolute inset-x-0 top-0 -z-10 h-[32rem]" aria-hidden="true"></div>
	<div class="page-x max-w-5xl! py-6 lg:py-10">
		<section class="vip-hero" aria-labelledby="vip-title">
			<span class="vip-crown" aria-hidden="true"><Icon name="crown" size={32} /></span>
			<h1 id="vip-title" class="mt-5 text-balance text-3xl font-extrabold tracking-tight lg:text-4xl">{i18n.t('vip.title')}</h1>
			<p class="mx-auto mt-3 max-w-xl text-[15px] leading-6 text-fg-muted">{i18n.t('vip.subtitle')}</p>

			{#if session.status === 'user'}
				<aside class="vip-status" data-active={vipStatus?.is_vip ? 'true' : 'false'} role="status" aria-live="polite" aria-atomic="true">
					<span class="vip-status-icon" aria-hidden="true"><Icon name={vipStatus?.is_vip ? 'circle-check' : 'crown'} size={20} /></span>
					<span class="min-w-0 text-start">
						<span class="block text-sm font-bold">{membershipTitle()}</span>
						<span class="block text-xs leading-5 text-fg-muted">{membershipDescription()}</span>
					</span>
				</aside>
			{/if}
		</section>

		<section class="mt-4" aria-label={i18n.t('vip.bannerTitle')}>
			<ul class="grid grid-cols-2 gap-3 text-start md:grid-cols-4">
				{#each PERKS as perk (perk.key)}
					<li class="vip-perk">
						<Icon name={perk.icon} size={21} class="text-vip-fg" />
						<p class="mt-3 text-sm font-semibold leading-5">{i18n.t(perk.key)}</p>
					</li>
				{/each}
			</ul>
		</section>

		{#if plans === null}
			<section class="mt-7 grid gap-3 sm:grid-cols-3" aria-busy="true" aria-label={i18n.t('vip.title')}>
				{#each { length: 3 }, index (index)}<span class="skeleton h-52 rounded-panel"></span>{/each}
			</section>
		{:else if loadFailed}
			<section class="mt-7 rounded-panel border border-dashed border-line bg-surface p-8 text-center" role="alert">
				<p class="text-lg font-bold">{i18n.t('vip.loadFailed')}</p>
				<button type="button" class="btn btn-ghost mt-6" onclick={() => void loadPlans()}>{i18n.t('vip.retry')}</button>
			</section>
		{:else if plans.length > 0}
			<section class="mt-7" aria-labelledby="vip-plans-title">
				<h2 id="vip-plans-title" class="sr-only">{i18n.t('vip.title')}</h2>
				<ul class="grid gap-3 text-start sm:grid-cols-3">
					{#each plans as plan (plan.id)}
						{@const badge = planBadge(plan)}
						<li>
							<button
								type="button"
								class="vip-plan"
								data-interactive={mockEnabled ? 'true' : 'false'}
								data-recommended={plan.recommended ? 'true' : 'false'}
								data-selected={selectedPlan?.id === plan.id ? 'true' : 'false'}
								aria-pressed={mockEnabled ? selectedPlan?.id === plan.id : undefined}
								disabled={!mockEnabled || planSelectionLocked()}
								onclick={() => selectPlan(plan)}
							>
								{#if badge}<span class="badge badge-md badge-vip absolute end-4 top-4">{badge}</span>{/if}
								<span class="vip-plan-name">{planName(plan)}</span>
								<span class="mt-1 text-xs text-fg-subtle">{planDays(plan)}</span>
								<span class="vip-plan-price mt-5">{cny(plan.price_fen)}</span>
								{#if plan.list_price_fen > plan.price_fen}
									<span class="mt-1 text-xs text-fg-subtle line-through">{cny(plan.list_price_fen)}</span>
								{/if}
								{#if mockEnabled}
									<span class="vip-plan-action mt-auto pt-6">
										{#if selectedPlan?.id === plan.id}<Icon name="circle-check" size={17} />{/if}
										{selectedPlan?.id === plan.id ? i18n.t('vip.mockSelected', { plan: planName(plan) }) : i18n.t('vip.mockChoose')}
									</span>
								{/if}
							</button>
						</li>
					{/each}
				</ul>
			</section>

			{#if mockEnabled && selectedPlan}
				<section class="vip-payment mt-6 text-start" aria-labelledby="mock-payment-title">
					<div class="flex items-start gap-3">
						<span class="vip-payment-icon" aria-hidden="true"><Icon name="wallet" size={20} /></span>
						<div class="min-w-0">
							<h2 id="mock-payment-title" class="text-base font-bold">{i18n.t('vip.mockSelected', { plan: planName(selectedPlan) })}</h2>
							<p class="mt-1 text-sm text-fg-muted">{planDays(selectedPlan)} · {cny(selectedPlan.price_fen)}</p>
						</div>
					</div>

					{#if selectedPlan.channels.length === 0}
						<p class="mt-5 text-sm leading-6 text-fg-muted" role="alert">{i18n.t('vip.soonDesc')}</p>
					{:else}
						<label for="mock-channel" class="mt-5 block text-sm font-medium">{i18n.t('vip.chooseChannel')}</label>
						<select id="mock-channel" class="field mt-2 max-w-sm text-sm" bind:value={selectedChannel} disabled={planSelectionLocked()}>
							{#each selectedPlan.channels as channel (channel)}
								<option value={channel}>{channelLabel(channel)}</option>
							{/each}
						</select>

						<div class="vip-payment-actions mt-5" role="status" aria-atomic="true">
							{#if mockOrder?.status === 'paid'}
								<p class="flex items-center gap-2 text-sm font-medium text-ok"><Icon name="circle-check" size={18} />{i18n.t('vip.mockPaid')}</p>
								{#if returnPath}<a href={returnPath} class="btn btn-ghost">{i18n.t('vip.returnToPlayback')}</a>{/if}
							{:else if mockAction}
								<button type="button" class="btn btn-vip" onclick={() => void completeMockPayment()} disabled={paymentPending}>{paymentPending ? i18n.t('vip.mockPending') : i18n.t('vip.mockComplete')}</button>
							{:else}
								<button type="button" class="btn btn-vip" onclick={() => void createMockOrder()} disabled={paymentPending || session.status === 'unknown' || !selectedChannel}>{paymentPending ? i18n.t('vip.mockPending') : i18n.t('vip.mockStart')}</button>
							{/if}
							{#if paymentFailed}<p class="text-sm text-danger" role="alert">{i18n.t('vip.mockFailed')}</p>{/if}
						</div>
					{/if}
				</section>
			{:else}
				<div class="mt-7 text-center" role="status" aria-atomic="true">
					<button type="button" class="btn btn-lg btn-vip" disabled>{i18n.t('vip.soon')}</button>
				</div>
			{/if}
		{:else}
			<section class="mt-7 rounded-panel border border-dashed border-line bg-surface p-8 text-center">
				<p class="text-lg font-bold">{i18n.t('vip.soonTitle')}</p>
				<p class="mx-auto mt-2 max-w-md text-sm leading-6 text-fg-muted">{i18n.t('vip.soonDesc')}</p>
				<a href={i18n.path('/explore')} class="btn btn-ghost mt-6">{i18n.t('home.goExplore')}</a>
			</section>
		{/if}

		<p class="vip-notice"><Icon name="shield-check" size={17} />{i18n.t('vip.notice')}</p>
	</div>
</div>
