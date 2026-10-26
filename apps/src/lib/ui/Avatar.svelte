<script lang="ts">
	import { titleHue } from '#lib/media/image.ts';

	type Props = { name: string; size?: number; class?: string };
	let { name, size = 40, class: className = '' }: Props = $props();

	// 头像上传随 S2-9 / S6-11；目前按昵称首字生成，颜色由昵称决定、每次一致
	const initial = $derived([...(name.trim() || '?')][0]?.toUpperCase() ?? '?');
	// 色相只取蓝、紫、粉、红一段（200°–30°），避开黄绿：白字在这些底色上都能保持足够对比度
	const hue = $derived((200 + (titleHue(name || '?') % 190)) % 360);
</script>

<span
	class="inline-grid shrink-0 place-items-center rounded-full font-bold text-white select-none {className}"
	style:width="{size}px"
	style:height="{size}px"
	style:font-size="{Math.round(size * 0.42)}px"
	style:background="linear-gradient(135deg, hsl({hue} 70% 45%), hsl({(hue + 30) % 360} 72% 32%))"
	aria-hidden="true"
>
	{initial}
</span>
