<script lang="ts">
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from '#lib/ui/Icon.svelte';

	const i18n = getI18n();
	let show = $state(false);

	$effect(() => {
		const onScroll = () => {
			show = window.scrollY > 900;
		};
		window.addEventListener('scroll', onScroll, { passive: true });
		return () => window.removeEventListener('scroll', onScroll);
	});

	function top() {
		const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
		window.scrollTo({ top: 0, behavior: reduce ? 'auto' : 'smooth' });
	}
</script>

<button
	type="button"
	class="fixed end-4 bottom-[calc(var(--tabbar-h)+var(--safe-bottom)+1rem)] z-30 grid size-11 place-items-center rounded-full border border-line bg-overlay/90 text-fg shadow-pop backdrop-blur transition-[opacity,transform] duration-200 hover:bg-raised lg:end-8 lg:bottom-8 {show
		? 'translate-y-0 opacity-100'
		: 'pointer-events-none translate-y-3 opacity-0'}"
	aria-label={i18n.t('common.backToTop')}
	tabindex={show ? 0 : -1}
	aria-hidden={show ? undefined : 'true'}
	onclick={top}
>
	<Icon name="arrow-up" size={20} />
</button>
