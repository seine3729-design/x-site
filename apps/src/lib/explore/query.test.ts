import { describe, expect, it } from 'vitest';
import { exploreParams, exploreSearch, parseExplore, toggleTerm, type ExploreQuery } from './query.ts';

const parse = (search: string) => parseExplore(new URLSearchParams(search));
const base: ExploreQuery = { terms: [], tag: '', finished: '', window: 'all', sort: 'latest' };

describe('parseExplore', () => {
	it('空查询串给出默认条件', () => {
		expect(parse('')).toEqual(base);
	});

	it('认得合法的排序、时间窗与完结状态', () => {
		expect(parse('sort=hot&window=7d&finished=true')).toEqual({ ...base, sort: 'hot', window: '7d', finished: 'true' });
	});

	it('非法取值一律落回默认（用户可以手改地址栏）', () => {
		expect(parse('sort=price&window=1y&finished=maybe')).toEqual(base);
	});

	it('terms 只收 维度:取值 形式，去重并最多 8 个', () => {
		expect(parse('terms=genre:urban, genre:sweet ,genre:urban').terms).toEqual(['genre:urban', 'genre:sweet']);
		expect(parse('terms=genre:urban,bad,:x,genre%3A,UPPER:CASE').terms).toEqual(['genre:urban']);
		const many = Array.from({ length: 12 }, (_, i) => `genre:t${i}`).join(',');
		expect(parse(`terms=${many}`).terms).toHaveLength(8);
	});

	it('tag 必须是 slug', () => {
		expect(parse('tag=ceo-1').tag).toBe('ceo-1');
		expect(parse('tag=%E4%B8%AD%E6%96%87').tag).toBe('');
		expect(parse('tag=-lead').tag).toBe('');
	});
});

describe('exploreSearch', () => {
	it('默认条件不带任何参数', () => {
		expect(exploreSearch(base)).toBe('');
	});

	it('同一组条件只有一个地址：键按字母序、terms 内部也排序', () => {
		const a = exploreSearch({ ...base, sort: 'hot', terms: ['region:cn', 'genre:urban'], window: '7d' });
		const b = exploreSearch({ ...base, sort: 'hot', terms: ['genre:urban', 'region:cn'], window: '7d' });
		expect(a).toBe(b);
		expect(a).toBe('?sort=hot&terms=genre%3Aurban%2Cregion%3Acn&window=7d');
	});

	it('和 parseExplore 互逆', () => {
		const query: ExploreQuery = { terms: ['genre:sweet'], tag: 'ceo', finished: 'false', window: '30d', sort: 'recommend' };
		expect(parse(exploreSearch(query))).toEqual(query);
	});
});

describe('toggleTerm', () => {
	it('单选维度：新取值替换同维度旧值，其他维度保留', () => {
		const query = { ...base, terms: ['genre:urban', 'region:cn'] };
		expect(toggleTerm(query, 'genre', 'sweet', false).terms).toEqual(['region:cn', 'genre:sweet']);
	});

	it('多选维度：追加，再点一次去掉', () => {
		const once = toggleTerm({ ...base, terms: ['genre:urban'] }, 'genre', 'sweet', true);
		expect(once.terms).toEqual(['genre:urban', 'genre:sweet']);
		expect(toggleTerm(once, 'genre', 'sweet', true).terms).toEqual(['genre:urban']);
	});

	it('传 null 表示「全部」，清掉该维度', () => {
		const query = { ...base, terms: ['genre:urban', 'genre:sweet', 'region:cn'] };
		expect(toggleTerm(query, 'genre', null, true).terms).toEqual(['region:cn']);
	});

	it('多选也不超过 8 个', () => {
		const query = { ...base, terms: Array.from({ length: 8 }, (_, i) => `genre:t${i}`) };
		expect(toggleTerm(query, 'genre', 'more', true).terms).toHaveLength(8);
	});
});

describe('exploreParams', () => {
	it('只传非默认条件，terms 按选择顺序用逗号拼', () => {
		expect(exploreParams(base)).toEqual({ sort: 'latest' });
		expect(exploreParams({ terms: ['genre:urban', 'region:cn'], tag: 'ceo', finished: 'true', window: '7d', sort: 'hot' })).toEqual({
			sort: 'hot',
			terms: 'genre:urban,region:cn',
			tag: 'ceo',
			finished: 'true',
			window: '7d'
		});
	});
});
