<script lang="ts">
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import type { VipPlan, VipPlansResponse } from '#lib/api/types.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import { session } from '#lib/stores/session.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';

	type MockOrder = { order_no: string; status: string; expire_at: string };
	type MockAction = { type: 'mock'; complete_endpoint: string };

	const i18n = getI18n();
	let plans = $state<VipPlan[] | null>(null);
	let client = $state<VipPlansResponse['client']>('web_pc');
	let mockEnabled = $state(false);
	let loadFailed = $state(false);
	let selectedPlan = $state<VipPlan | null>(null);
	let selectedChannel = $state<string>('');
	let mockOrder = $state<MockOrder | null>(null);
	let mockAction = $state<MockAction | null>(null);
	let paymentPending = $state(false);
	let paymentFailed = $state(false);

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

	onMount(() => {
		void loadPlans();
	});

	const PERKS = [
		{ icon: 'film', key: 'vip.perkAll' },
		{ icon: 'zap', key: 'vip.perkEarly' },
		{ icon: 'gauge', key: 'vip.perkHd' },
		{ icon: 'shield-check', key: 'vip.perkNoAds' }
	] as const;

	function planName(plan: VipPlan) {
		return i18n.t(`vip.plan.${plan.code}`);
	}

	function planBadge(plan: VipPlan) {
		if (plan.badge_key) return i18n.t(plan.badge_key);
		return plan.recommended ? i18n.t('vip.recommended') : '';
	}

	function planDays(plan: VipPlan) {
		return i18n.t('vip.durationDays', { n: plan.duration_days });
	}

	function cny(fen: number) {
		return new Intl.NumberFormat(i18n.locale, { style: 'currency', currency: 'CNY', maximumFractionDigits: 2 }).format(fen / 100);
	}

	function channelLabel(channel: string) {
		return i18n.t(`vip.channel.${channel}`);
	}

	function selectPlan(plan: VipPlan) {
		selectedPlan = plan;
		selectedChannel = plan.channels[0] ?? '';
		mockOrder = null;
		mockAction = null;
		paymentFailed = false;
	}

	async function createMockOrder() {
		if (!selectedPlan || !selectedChannel) return;
		if (session.status !== 'user') {
			window.location.assign(i18n.path('/login?next=%2Fvip'));
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
				return_path: '/vip',
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
			const order = await api.post<MockOrder>(mockAction.complete_endpoint);
			mockOrder = order;
			await session.refresh(true);
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

<div class="relative isolate overflow-hidden">
	<div class="absolute inset-x-0 top-0 -z-10 h-96 bg-[radial-gradient(60%_100%_at_50%_0%,rgb(227_169_58/0.28),transparent)]" aria-hidden="true"></div>
	<div class="page-x max-w-4xl! pt-8 text-center lg:pt-14">
		<span class="mx-auto mb-5 grid size-16 place-items-center rounded-3xl bg-gradient-to-br from-[#f7d27a] to-[#e3a93a] text-on-vip shadow-[0_16px_40px_-12px_rgb(227_169_58/0.7)]">
			<Icon name="crown" size={32} />
		</span>
		<h1 class="text-3xl font-extrabold tracking-tight lg:text-4xl">{i18n.t('vip.title')}</h1>
		<p class="mx-auto mt-3 max-w-md text-[15px] text-fg-muted">{i18n.t('vip.subtitle')}</p>

		<ul class="mt-10 grid grid-cols-2 gap-3 text-start md:grid-cols-4">
			{#each PERKS as perk (perk.key)}
				<li class="rounded-panel border border-line bg-surface p-4">
					<Icon name={perk.icon} size={22} class="text-vip-fg" />
					<p class="mt-3 text-sm font-semibold">{i18n.t(perk.key)}</p>
				</li>
			{/each}
		</ul>

		{#if plans === null}
			<div class="mt-10 grid gap-3 sm:grid-cols-3" aria-hidden="true">
				{#each { length: 3 }, index (index)}<span class="skeleton h-40 rounded-panel"></span>{/each}
			</div>
		{:else if loadFailed}
			<div class="mt-10 rounded-panel border border-dashed border-line p-8" role="alert">
				<p class="text-lg font-bold">{i18n.t('vip.loadFailed')}</p>
				<button type="button" class="btn btn-ghost mt-6" onclick={() => void loadPlans()}>{i18n.t('vip.retry')}</button>
			</div>
		{:else if plans.length > 0}
			<ul class="mt-10 grid gap-3 text-start sm:grid-cols-3" aria-label={i18n.t('vip.title')}>
				{#each plans as plan (plan.id)}
					{@const badge = planBadge(plan)}
					<li class="relative flex min-h-48 flex-col rounded-panel border p-5 {plan.recommended ? 'border-[#e3a93a] bg-[#e3a93a]/10' : 'border-line bg-surface'}">
						{#if badge}<span class="absolute end-4 top-4 rounded-full bg-brand px-2 py-0.5 text-[11px] font-bold text-on-brand">{badge}</span>{/if}
						<p class="text-base font-bold">{planName(plan)}</p>
						<p class="mt-1 text-xs text-fg-subtle">{planDays(plan)}</p>
						<p class="mt-3 text-3xl font-extrabold tabular">{cny(plan.price_fen)}</p>
						{#if plan.list_price_fen > plan.price_fen}
							<p class="text-xs text-fg-subtle line-through">{cny(plan.list_price_fen)}</p>
						{/if}
						{#if mockEnabled}
							<button type="button" class="btn btn-ghost mt-auto pt-5" onclick={() => selectPlan(plan)}>{i18n.t('vip.mockChoose')}</button>
						{/if}
					</li>
				{/each}
			</ul>

			{#if mockEnabled && selectedPlan}
				<section class="mt-6 rounded-panel border border-dashed border-[#e3a93a]/50 bg-surface p-5 text-start" aria-labelledby="mock-payment-title">
					<h2 id="mock-payment-title" class="text-base font-bold">{i18n.t('vip.mockSelected', { plan: planName(selectedPlan) })}</h2>
					<label for="mock-channel" class="mt-4 block text-sm font-medium">{i18n.t('vip.chooseChannel')}</label>
					<select id="mock-channel" class="mt-2 h-10 w-full rounded-md border border-line bg-canvas px-3 text-sm sm:max-w-sm" bind:value={selectedChannel} disabled={paymentPending}>
						{#each selectedPlan.channels as channel (channel)}
							<option value={channel}>{channelLabel(channel)}</option>
						{/each}
					</select>
					<div class="mt-4 flex flex-wrap gap-2" role="status" aria-atomic="true">
						{#if mockOrder?.status === 'paid'}
							<p class="text-sm font-medium text-success">{i18n.t('vip.mockPaid')}</p>
						{:else if mockAction}
							<button type="button" class="btn btn-vip" onclick={() => void completeMockPayment()} disabled={paymentPending}>{paymentPending ? i18n.t('vip.mockPending') : i18n.t('vip.mockComplete')}</button>
						{:else}
							<button type="button" class="btn btn-vip" onclick={() => void createMockOrder()} disabled={paymentPending}>{paymentPending ? i18n.t('vip.mockPending') : i18n.t('vip.mockStart')}</button>
						{/if}
						{#if paymentFailed}<p class="text-sm text-danger">{i18n.t('vip.mockFailed')}</p>{/if}
					</div>
				</section>
			{:else}
				<div class="mt-8" role="status" aria-atomic="true">
					<button type="button" class="btn btn-lg btn-vip" disabled>{i18n.t('vip.soon')}</button>
				</div>
			{/if}
		{:else}
			<div class="mt-10 rounded-panel border border-dashed border-line p-8">
				<p class="text-lg font-bold">{i18n.t('vip.soonTitle')}</p>
				<p class="mt-2 text-sm text-fg-muted">{i18n.t('vip.soonDesc')}</p>
				<a href={i18n.path('/explore')} class="btn btn-ghost mt-6">{i18n.t('home.goExplore')}</a>
			</div>
		{/if}
		<p class="mt-8 text-xs leading-5 text-fg-subtle">{i18n.t('vip.notice')}</p>
	</div>
</div>
