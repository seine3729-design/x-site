type Bucket = {
	tokens: number;
	updatedAt: number;
};

const buckets = new Map<string, Bucket>();
const MAX_BUCKETS = 5000;

export type RateDecision = {
	ok: boolean;
	retryAfterSeconds: number;
};

function prune(now: number) {
	if (buckets.size <= MAX_BUCKETS) {
		return;
	}

	for (const [key, bucket] of buckets) {
		if (now - bucket.updatedAt > 120_000) {
			buckets.delete(key);
		}

		if (buckets.size <= MAX_BUCKETS) {
			return;
		}
	}
}

export function takeToken(key: string, limit: number, windowMs: number): RateDecision {
	const now = Date.now();
	prune(now);

	const refillPerMs = limit / windowMs;
	const existing = buckets.get(key);
	const bucket = existing ?? { tokens: limit, updatedAt: now };

	if (!existing) {
		buckets.set(key, bucket);
	}

	const elapsed = now - bucket.updatedAt;
	bucket.tokens = Math.min(limit, bucket.tokens + elapsed * refillPerMs);
	bucket.updatedAt = now;

	if (bucket.tokens < 1) {
		const missing = 1 - bucket.tokens;

		return {
			ok: false,
			retryAfterSeconds: Math.max(1, Math.ceil(missing / refillPerMs / 1000))
		};
	}

	bucket.tokens -= 1;

	return { ok: true, retryAfterSeconds: 0 };
}
