<script lang="ts">
	import { onMount } from 'svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import { getSite } from '#lib/site.ts';

	const i18n = getI18n();
	const site = $derived(getSite());
	const gate = $derived(site.ageGate);

	// SSR 在开关打开时总是输出遮罩；本机确认过的由 app.html 的内联脚本写 data-age-ok，CSS 直接隐藏，首帧不闪
	let confirmed = $state(false);
	let confirmButton = $state<HTMLButtonElement>();

	onMount(() => {
		confirmed = document.documentElement.hasAttribute('data-age-ok');
		if (!confirmed) {
			confirmButton?.focus();
		}
	});

	function confirm() {
		const days = Math.min(365, Math.max(1, Number(gate.remember_days) || 30));
		try {
			localStorage.setItem('xs_age_ok', String(Date.now() + days * 86_400_000));
		} catch {
			// 写不进去：本次会话内仍然放行
		}
		document.documentElement.setAttribute('data-age-ok', '');
		confirmed = true;
	}

	const denyUrl = $derived(/^https?:\/\//i.test(gate.deny_url ?? '') ? gate.deny_url! : 'about:blank');
</script>

{#if gate.enabled && !confirmed}
	<!-- 遮罩做成毛玻璃：背景透出来但强模糊 + 压暗，既能看出「后面还有内容」又聚焦到弹窗（之前 bg-canvas/95 几乎全盖，看不出虚化） -->
	<div
		class="age-gate fixed inset-0 z-[100] grid place-items-center bg-black/60 p-6 backdrop-blur-xl"
		role="dialog"
		aria-modal="true"
		aria-labelledby="age-gate-title"
	>
		<div class="w-full max-w-sm animate-rise rounded-panel border border-line bg-overlay p-7 text-center shadow-pop">
			<!-- 成人内容年龄门：用「18+」圆形徽章 + 固定琥珀警示色（独立于白标配色，年龄门的通用视觉） -->
			<span class="mx-auto mb-5 grid size-20 place-items-center rounded-full border-[5px] border-amber-400 text-amber-400" aria-hidden="true">
				<span class="text-2xl leading-none font-black tracking-tight tabular">18<span class="align-top text-sm">+</span></span>
			</span>
			<h2 id="age-gate-title" class="text-xl font-bold">{gate.title || i18n.t('age.title')}</h2>
			<p class="mt-3 text-sm leading-6 text-fg-muted">{gate.desc || i18n.t('age.desc')}</p>
			<div class="mt-7 flex flex-col gap-3">
				<button
					bind:this={confirmButton}
					type="button"
					class="btn btn-lg w-full bg-amber-400 font-bold text-amber-950 hover:bg-amber-300"
					onclick={confirm}
				>
					{gate.confirm_text || i18n.t('age.confirm')}
				</button>
				<a href={denyUrl} class="btn btn-ghost w-full" rel="noopener noreferrer">{gate.deny_text || i18n.t('age.deny')}</a>
			</div>
		</div>
	</div>
{/if}

<style>
	:global(html[data-age-ok]) .age-gate {
		display: none;
	}
</style>
