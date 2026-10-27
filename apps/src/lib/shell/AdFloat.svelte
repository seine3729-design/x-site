<script lang="ts">
	/**
	 * APP 下载浮窗广告位（`app_download`，docs/04 C.5）：素材取到了才出现，关掉后当天不再出现。
	 * 固定在右下角、让开手机底栏与安全区，不挡住主内容。
	 */
	import AdSlot from '#lib/home/AdSlot.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import { dismissedToday, dismissToday } from './ad-dismiss.ts';

	const i18n = getI18n();
	let hasAd = $state(false);
	let closed = $state(dismissedToday('float'));
</script>

{#if !closed}
	<div
		class="pointer-events-none fixed end-3 bottom-[calc(var(--tabbar-h)+var(--safe-bottom)+0.75rem)] z-30 w-40 lg:bottom-4 lg:w-48 {hasAd
			? ''
			: 'invisible'}"
	>
		<div class="pointer-events-auto relative">
			<AdSlot slot="app_download" aspect={[320, 180]} onresolved={(v) => (hasAd = v)} />
			{#if hasAd}
				<button
					type="button"
					class="absolute -end-2 -top-2 grid size-7 place-items-center rounded-full bg-overlay text-fg-muted shadow-pop ring-1 ring-line hover:text-fg"
					aria-label={i18n.t('ad.close')}
					onclick={() => {
						closed = true;
						dismissToday('float');
					}}
				>
					<Icon name="x" size={14} />
				</button>
			{/if}
		</div>
	</div>
{/if}
