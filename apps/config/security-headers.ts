type Header = { key: string; value: string };

/**
 * 从配置里的地址取出 scheme://host:port，填错的地址直接忽略（CSP 里写坏值会让整条策略失效）。
 */
function originsOf(urls: readonly string[]): string[] {
	const out = new Set<string>();

	for (const url of urls) {
		try {
			const parsed = new URL(url);

			if (parsed.protocol === 'https:' || parsed.protocol === 'http:') {
				out.add(parsed.origin);
			}
		} catch {
			// 不是合法地址，跳过
		}
	}

	return [...out];
}

/**
 * 文档响应的安全头（docs/10 第 4 节）。
 *
 * connect-src 要包含图片与视频切片的域名池（docs/04 C.8 第 322 行、D 节）：
 * - 封面走 BlobImage 的 fetch + blob:，受 connect-src 约束而不是 img-src，域名不在其中就一张封面都出不来；
 * - 视频切片由 hls.js 用 XHR 从线路 CDN 取（清单与密钥是本站地址，不受影响）。
 *
 * 两个池子都来自部署配置（IMAGE_BASE_URLS、MEDIA_CONNECT_ORIGINS）。后台线路表是运行时数据，
 * 这里列不到的域名在生产会被拦掉；要让它跟着线路表自动变，得把域名池放进 site-config 再按请求生成 CSP。
 * 另外后台可以把封面填成任意完整地址，那类域名同样需要登记。
 */
export function documentSecurityHeaders(
	isProd: boolean,
	cdn: string,
	imageBases: readonly string[] = [],
	mediaOrigins: readonly string[] = []
): Header[] {
	const origin = cdn.replace(/\/$/, '');
	const cdnSource = origin ? ` ${origin}` : '';
	const imageSource = originsOf(imageBases);
	const imageList = imageSource.length > 0 ? ` ${imageSource.join(' ')}` : '';
	const mediaSource = originsOf(mediaOrigins).filter((value) => !imageSource.includes(value));
	const mediaList = mediaSource.length > 0 ? ` ${mediaSource.join(' ')}` : '';
	const headers: Header[] = [
		{ key: 'X-Content-Type-Options', value: 'nosniff' },
		{ key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
		{ key: 'X-Frame-Options', value: 'DENY' },
		{ key: 'Permissions-Policy', value: 'camera=(), microphone=(), geolocation=(), payment=()' },
		{ key: 'X-DNS-Prefetch-Control', value: 'on' },
		{ key: 'Cross-Origin-Opener-Policy', value: 'same-origin' }
	];

	if (!isProd) {
		return headers;
	}

	headers.push(
		{
			key: 'Strict-Transport-Security',
			value: 'max-age=31536000; includeSubDomains'
		},
		{
			key: 'Content-Security-Policy',
			value: [
				"default-src 'self'",
				`script-src 'self' 'unsafe-inline'${cdnSource}`,
				`style-src 'self' 'unsafe-inline'${cdnSource}`,
				// 封面解码后挂成 blob:；后台可能把 favicon / Logo 填成图片线路上的完整地址
				`img-src 'self' blob:${cdnSource}${imageList}`,
				// 播放器通过 MSE 把视频挂成 blob: 地址；hls.js 的转封装 Worker 也来自 blob:
				"media-src 'self' blob:",
				"worker-src 'self' blob:",
				`font-src 'self'${cdnSource}`,
				// 浏览器调 /api/v1/* 是同源；图片按线路域名 fetch；hls.js 的切片走视频线路域名
				`connect-src 'self'${cdnSource}${imageList}${mediaList}`,
				"object-src 'none'",
				"base-uri 'self'",
				"form-action 'self'",
				"frame-ancestors 'none'",
				'upgrade-insecure-requests'
			].join('; ')
		}
	);

	return headers;
}
