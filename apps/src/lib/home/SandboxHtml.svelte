<script lang="ts">
	import { onMount } from 'svelte';

	type Props = {
		html: string;
		title: string;
		minHeight?: number;
		/** 正文排版（静态页）：更大的行高、标题层级与列表 / 引用 / 表格样式 */
		prose?: boolean;
		/** 框内底色取哪个令牌：外层放在卡片里时用 surface，直接铺在页面上时用 canvas */
		surface?: 'surface' | 'canvas';
	};
	let { html, title, minHeight = 120, prose = false, surface = 'canvas' }: Props = $props();

	// 后台富文本与自定义 HTML（已按白名单清洗）只进沙箱 iframe（docs/04 F 节）：允许脚本但不给同源权限，
	// 读不到本站 Cookie、存储与页面 DOM；高度由框内一小段脚本经 postMessage 告诉外层，避免内部滚动条。
	// 配色取当前主题的 CSS 变量，所以只在浏览器里渲染（SSR 不知道用户选的主题）。
	let frame = $state<HTMLIFrameElement>();
	let measured = $state(0);
	const height = $derived(Math.min(20_000, Math.max(minHeight, measured)));
	type Colors = { fg: string; muted: string; subtle: string; link: string; line: string; bg: string; raised: string; dark: boolean };
	let colors = $state<Colors | null>(null);

	onMount(() => {
		const style = getComputedStyle(document.documentElement);
		const read = (name: string) => style.getPropertyValue(name).trim();
		colors = {
			fg: read('--fg'),
			muted: read('--fg-muted'),
			subtle: read('--fg-subtle'),
			link: read('--brand-fg'),
			line: read('--line-strong'),
			// iframe 的画布不会继承父页的透明背景（深色页里会露出浏览器默认的白底），所以显式填上底色
			bg: read(surface === 'surface' ? '--surface' : '--canvas'),
			raised: read('--raised'),
			dark: !document.documentElement.classList.contains('light')
		};
	});

	const srcdoc = $derived.by(() => {
		if (!colors) {
			return '';
		}
		const c = colors;
		const base =
			`:root{color-scheme:${c.dark ? 'dark' : 'light'}}` +
			`html,body{margin:0;background:${c.bg};color:${c.fg};` +
			// 只用 break-word：overflow-wrap:anywhere 会参与 min-content 计算，实测让框内正文被压成每行一个字
			`font:15px/1.8 system-ui,-apple-system,'PingFang SC','Noto Sans SC',sans-serif;word-wrap:break-word}` +
			`img,video{max-width:100%;height:auto;border-radius:12px}` +
			`a{color:${c.link};text-decoration:none}a:hover{text-decoration:underline}` +
			`table{border-collapse:collapse;width:100%;margin:1.2em 0;font-size:.95em}` +
			`th,td{border:1px solid ${c.line};padding:8px 12px;text-align:start}` +
			`th{background:${c.raised};font-weight:600;color:${c.fg}}`;
		const article = prose
			? `*:first-child{margin-top:0}*:last-child{margin-bottom:0}` +
				`h1,h2,h3,h4{color:${c.fg};line-height:1.4;font-weight:700}` +
				`h1{font-size:1.5em;margin:1.6em 0 .7em}` +
				// 二级标题加一条品牌色竖线：长条款页面扫起来有锚点
				`h2{font-size:1.2em;margin:2em 0 .7em;padding-inline-start:.6em;border-inline-start:3px solid ${c.link}}` +
				`h3{font-size:1.05em;margin:1.6em 0 .5em}h4{font-size:1em;margin:1.4em 0 .4em}` +
				`p{color:${c.muted};margin:.9em 0}` +
				`ul,ol{color:${c.muted};margin:.9em 0;padding-inline-start:1.3em}` +
				`li{margin:.45em 0}li::marker{color:${c.link}}` +
				`strong,b{color:${c.fg};font-weight:600}` +
				`blockquote{margin:1.2em 0;padding:.1em 1.1em;border-inline-start:3px solid ${c.line};color:${c.subtle}}` +
				`code{background:${c.raised};padding:.1em .4em;border-radius:6px;font-size:.92em}` +
				`pre{background:${c.raised};padding:1em;border-radius:12px;overflow:auto}pre code{background:none;padding:0}` +
				`hr{border:0;border-top:1px solid ${c.line};margin:2em 0}` +
				`small{color:${c.subtle}}`
			: '';
		return (
			`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><base target="_blank">` +
			`<style>${base}${article}</style></head><body>${html}` +
			`<script>new ResizeObserver(function(){parent.postMessage({xsFrameHeight:document.documentElement.scrollHeight},'*')}).observe(document.documentElement)<` +
			'/script></body></html>'
		);
	});

	$effect(() => {
		const onMessage = (event: MessageEvent) => {
			if (event.source !== frame?.contentWindow) {
				return;
			}
			const value = (event.data as { xsFrameHeight?: unknown })?.xsFrameHeight;
			if (typeof value === 'number' && Number.isFinite(value)) {
				measured = Math.ceil(value);
			}
		};
		window.addEventListener('message', onMessage);
		return () => window.removeEventListener('message', onMessage);
	});
</script>

{#if colors}
	<iframe
		bind:this={frame}
		{title}
		{srcdoc}
		sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox"
		referrerpolicy="no-referrer"
		class="block w-full border-0 bg-transparent"
		style:height="{height}px"
	></iframe>
{:else}
	<div class="skeleton w-full rounded-2xl" style:height="{minHeight}px" aria-hidden="true"></div>
{/if}
