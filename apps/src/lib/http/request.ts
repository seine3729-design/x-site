const AUTOMATION_AGENT =
	/(scrapy|python-requests|aiohttp|httpclient|go-http-client|semrush|ahrefs|bytespider|petalbot|headlesschrome)/i;

export function clientIp(headers: Headers): string {
	const forwarded = headers.get('x-forwarded-for');

	if (forwarded) {
		return forwarded.split(',')[0]?.trim() || 'unknown';
	}

	return headers.get('x-real-ip') ?? 'local';
}

export function isAutomationAgent(userAgent: string | null): boolean {
	if (!userAgent) {
		return true;
	}

	return AUTOMATION_AGENT.test(userAgent);
}

export function safeEqual(left: string, right: string): boolean {
	const a = new TextEncoder().encode(left);
	const b = new TextEncoder().encode(right);

	if (a.byteLength !== b.byteLength) {
		return false;
	}

	let diff = 0;

	for (let index = 0; index < a.byteLength; index += 1) {
		diff |= a[index]! ^ b[index]!;
	}

	return diff === 0;
}
