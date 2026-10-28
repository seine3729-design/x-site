import { defineEnvVars } from '@sveltejs/kit/env';

type AppEnvironment = 'development' | 'test' | 'production';

function appEnvironment(value: string | undefined): AppEnvironment {
	if (value === 'development' || value === 'test' || value === 'production') {
		return value;
	}

	throw new Error('环境标识必须是 development、test 或 production');
}

function optionalUrl(value: string | undefined): string {
	const trimmed = value?.replace(/\/$/, '') ?? '';

	if (!trimmed) {
		return '';
	}

	if (trimmed.startsWith('http://') || trimmed.startsWith('https://')) {
		return trimmed;
	}

	throw new Error('地址必须以 http:// 或 https:// 开头');
}

function optionalText(value: string | undefined): string {
	return value?.trim() ?? '';
}

function urlList(value: string | undefined): string[] {
	return (value ?? '')
		.split(',')
		.map((item) => item.trim())
		.filter(Boolean)
		.map((item) => {
			const url = optionalUrl(item);
			if (!url) {
				throw new Error(`图片线路地址无效：${item}`);
			}
			return url;
		});
}

function positiveInt(fallback: number) {
	return (value: string | undefined): number => {
		if (!value) {
			return fallback;
		}

		const parsed = Number(value);

		if (!Number.isFinite(parsed) || parsed <= 0) {
			throw new Error('配额必须是正数');
		}

		return parsed;
	};
}

export const variables = defineEnvVars({
	APP_ENV: {
		description: '当前环境标识，只在服务端读取',
		schema: appEnvironment
	},
	PUBLIC_APP_ENV: {
		public: true,
		static: true,
		description: '打进浏览器脚本的环境标识，构建时写入',
		schema: appEnvironment
	},
	PUBLIC_SITE_URL: {
		public: true,
		static: true,
		description: '站点地址。线上留空时，绝对链接用当次请求的 Host',
		schema: optionalUrl
	},
	CDN_ASSET_PREFIX: {
		description: '静态资源 CDN 域名，不要带末尾斜杠',
		schema: optionalUrl
	},
	EDGE_SHARED_SECRET: {
		description: '配置后，只有携带相同 x-edge-secret 的请求才能使用 CDN 写入的国家头',
		schema: optionalText
	},
	RATE_LIMIT_PAGE_PER_MIN: {
		description: '页面请求每分钟配额',
		schema: positiveInt(300)
	},
	RATE_LIMIT_READ_PER_MIN: {
		description: '读接口每分钟配额',
		schema: positiveInt(120)
	},
	RATE_LIMIT_WRITE_PER_MIN: {
		description: '写接口每分钟配额',
		schema: positiveInt(30)
	},
	RATE_LIMIT_BOT_PER_MIN: {
		description: '自动化客户端每分钟配额',
		schema: positiveInt(20)
	},
	SERVICE_INTERNAL_URL: {
		description: 'BFF 访问 Go 后端的内网地址。docker compose 中为 http://service:8080',
		schema: (value: string | undefined) => optionalUrl(value) || 'http://localhost:8080'
	},
	BFF_SERVICE_TOKEN: {
		description: 'BFF 调用后端时携带的服务令牌，与 service 的 BFF_SERVICE_TOKEN 一致。只放 .env.local 或部署密钥中',
		schema: optionalText
	},
	IMAGE_BASE_URLS: {
		description:
			'图片线路域名，逗号分隔、按优先级排列；封面等相对 key 拼在其后，首选加载失败换下一条（docs/12 第 6 节）。留空时只显示后台填的完整图片地址',
		schema: urlList
	},
	MEDIA_CONNECT_ORIGINS: {
		description:
			'视频切片线路域名池，逗号分隔，只用于生成生产环境 CSP 的 connect-src（docs/04 C.8、D 节）。hls.js 用 XHR 取切片，域名不在其中时生产环境放不出视频；内容是后台线路表里会用到的域名，新增线路后要同步这里',
		schema: urlList
	},
	IMAGE_RESIZE_ORIGINS: {
		description:
			'图片线路里可以由站点所在 Cloudflare 区域经 /cdn-cgi/image/ 按显示尺寸实时缩放的域名，逗号分隔（docs/08 第 5 节）。留空不缩放（本地开发、没接 Cloudflare 的部署）。站点域名的区域必须开启 Images → Transformations',
		schema: urlList
	}
});
