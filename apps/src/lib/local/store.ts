/**
 * 游客本机数据（docs/04 B.11）：观看历史、追剧、点赞存在 IndexedDB；登录后同一份数据作为账号的本机缓存。
 * IndexedDB 不可用（隐私模式、被禁用）时退化为只存在内存里，页面照常可用、刷新后丢失。
 * 读写都是异步的，单次操作只碰一条记录，不阻塞主线程。
 */
import type { DramaCard } from '#lib/api/types.ts';

export type LocalHistory = {
	drama_id: number;
	title: string;
	cover: string;
	episode_id: number;
	episode_number: number;
	position_sec: number;
	duration_sec: number;
	latest_episode: number;
	/** 毫秒时间戳 */
	updated_at: number;
};

export type LocalFollow = Pick<
	DramaCard,
	'id' | 'title' | 'cover_portrait' | 'latest_episode' | 'is_finished' | 'badge' | 'play_count' | 'score' | 'year'
> & { followed_at: number };

type StoreName = 'history' | 'follows' | 'likes';
type Row = Record<string, unknown>;

const DB_NAME = 'xsite-local';
const DB_VERSION = 1;
/** 与 /api/v1/me/local-merge 每类上限一致 */
export const LOCAL_LIMIT = 300;

const memory: Record<StoreName, Map<IDBValidKey, Row>> = {
	history: new Map(),
	follows: new Map(),
	likes: new Map()
};

let dbPromise: Promise<IDBDatabase | null> | null = null;

function openDb(): Promise<IDBDatabase | null> {
	if (typeof indexedDB === 'undefined') {
		return Promise.resolve(null);
	}
	dbPromise ??= new Promise((resolve) => {
		try {
			const request = indexedDB.open(DB_NAME, DB_VERSION);
			request.onupgradeneeded = () => {
				const db = request.result;
				if (!db.objectStoreNames.contains('history')) {
					db.createObjectStore('history', { keyPath: 'drama_id' }).createIndex('updated_at', 'updated_at');
				}
				if (!db.objectStoreNames.contains('follows')) {
					db.createObjectStore('follows', { keyPath: 'id' });
				}
				if (!db.objectStoreNames.contains('likes')) {
					db.createObjectStore('likes', { keyPath: 'key' });
				}
			};
			request.onsuccess = () => resolve(request.result);
			request.onerror = () => resolve(null);
			request.onblocked = () => resolve(null);
		} catch {
			resolve(null);
		}
	});
	return dbPromise;
}

function wrap<T>(request: IDBRequest<T>): Promise<T> {
	return new Promise((resolve, reject) => {
		request.onsuccess = () => resolve(request.result);
		request.onerror = () => reject(request.error);
	});
}

async function run<T>(
	name: StoreName,
	mode: IDBTransactionMode,
	fn: (store: IDBObjectStore) => IDBRequest<T>,
	fallback: () => T
): Promise<T> {
	const db = await openDb();
	if (!db) {
		return fallback();
	}
	try {
		return await wrap(fn(db.transaction(name, mode).objectStore(name)));
	} catch {
		return fallback();
	}
}

const all = <T>(name: StoreName) =>
	run<T[]>(name, 'readonly', (s) => s.getAll() as IDBRequest<T[]>, () => [...memory[name].values()] as T[]);
const get = <T>(name: StoreName, key: IDBValidKey) =>
	run<T | undefined>(name, 'readonly', (s) => s.get(key) as IDBRequest<T | undefined>, () => memory[name].get(key) as T);
const put = (name: StoreName, key: IDBValidKey, row: Row) => {
	memory[name].set(key, row);
	return run(name, 'readwrite', (s) => s.put(row), () => key);
};
const remove = (name: StoreName, key: IDBValidKey) => {
	memory[name].delete(key);
	return run(name, 'readwrite', (s) => s.delete(key), () => undefined);
};
const clear = (name: StoreName) => {
	memory[name].clear();
	return run(name, 'readwrite', (s) => s.clear(), () => undefined);
};

export const localHistory = {
	async list(): Promise<LocalHistory[]> {
		return (await all<LocalHistory>('history')).sort((a, b) => b.updated_at - a.updated_at);
	},
	get: (dramaId: number) => get<LocalHistory>('history', dramaId),
	async save(entry: LocalHistory) {
		await put('history', entry.drama_id, entry);
		const rows = await this.list();
		for (const old of rows.slice(LOCAL_LIMIT)) {
			await remove('history', old.drama_id);
		}
	},
	remove: (dramaId: number) => remove('history', dramaId),
	clear: () => clear('history')
};

export const localFollows = {
	async list(): Promise<LocalFollow[]> {
		return (await all<LocalFollow>('follows')).sort((a, b) => b.followed_at - a.followed_at);
	},
	async has(dramaId: number) {
		return (await get<LocalFollow>('follows', dramaId)) !== undefined;
	},
	add: (card: LocalFollow) => put('follows', card.id, card),
	remove: (dramaId: number) => remove('follows', dramaId)
};

export const localLikes = {
	async has(type: 'drama' | 'episode', id: number) {
		return (await get<Row>('likes', `${type}:${id}`)) !== undefined;
	},
	add: (type: 'drama' | 'episode', id: number) => put('likes', `${type}:${id}`, { key: `${type}:${id}`, type, id, at: Date.now() }),
	remove: (type: 'drama' | 'episode', id: number) => remove('likes', `${type}:${id}`)
};
