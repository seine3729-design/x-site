import { describe, expect, it } from 'vitest';
import { mergeDomains } from './domains.ts';

describe('mergeDomains', () => {
	it('下发的后台线路排在部署配置前面（相对 key 要优先按后台当前在用的线路拼）', () => {
		expect(mergeDomains(['https://cdn.example'], ['https://legacy.example'])).toEqual([
			'https://cdn.example',
			'https://legacy.example'
		]);
	});

	it('去重（末尾斜杠归一），两边配了同一个域名只留一个', () => {
		expect(mergeDomains(['https://cdn.example/'], ['https://cdn.example'])).toEqual(['https://cdn.example']);
	});

	it('保留 base_url 里的路径前缀：相对 key 要拼在它后面', () => {
		expect(mergeDomains(['https://cdn.example/x-site'])).toEqual(['https://cdn.example/x-site']);
	});

	// 后端下发的数据不能假设形状：旧版本后端没有 media 段，字段也可能是 null
	it.each([
		['没有 media 段', undefined],
		['字段是 null', null],
		['字段不是数组', 'https://cdn.example'],
		['数组里混了非字符串', [1, true, null]]
	])('%s 时退回部署配置而不是抛错', (_name, value) => {
		expect(mergeDomains(value, ['https://env.example'])).toEqual(['https://env.example']);
	});

	it('只认 http(s)：空串、相对路径与 javascript: 一律丢掉（CSP 里写坏值会让整条策略失效）', () => {
		expect(mergeDomains(['', '  ', '/img', '//cdn.example', 'javascript:alert(1)', 'https://ok.example'])).toEqual([
			'https://ok.example'
		]);
	});

	it('两边都空时给空数组：此时相对 key 拼不出地址，前台只会画占位图', () => {
		expect(mergeDomains(undefined, [])).toEqual([]);
	});
});
