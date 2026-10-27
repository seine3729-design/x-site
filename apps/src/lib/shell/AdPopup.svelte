<script lang="ts">
	/**
	 * 全站弹窗广告位（`global_popup`，docs/04 C.5）：素材取到了才弹，关掉后当天不再弹。
	 * 电脑居中、手机从底部滑上来，与分享面板共用 `#lib/ui/Sheet.svelte`。
	 */
	import AdSlot from '#lib/home/AdSlot.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import Sheet from '#lib/ui/Sheet.svelte';
	import { dismissedToday, dismissToday } from './ad-dismiss.ts';

	const i18n = getI18n();
	let open = $state(false);

	function onresolved(hasAd: boolean) {
		// 没素材就不弹；当天关过也不再弹
		if (hasAd && !dismissedToday('popup')) {
			open = true;
		}
	}
</script>

<!-- AdSlot 常驻挂载（dialog 关着时是 display:none，曝光的可见性判定不会误报），
     取到素材再把抽屉打开 -->
<Sheet center {open} title={i18n.t('ad.label')} onclose={() => {
	open = false;
	dismissToday('popup');
}}>
	<div class="px-4 pb-4">
		<AdSlot slot="global_popup" aspect={[900, 1200]} {onresolved} />
	</div>
</Sheet>
