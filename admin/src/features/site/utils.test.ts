import { describe, expect, it } from 'vitest'
import type { Block, SiteMenu, StaticPage } from './api'
import {
  activeCount,
  addressesToForm,
  blockHasList,
  blockPageLabel,
  blockSourceSummary,
  blockToForm,
  bodySummary,
  bodyWasSanitized,
  formToAddresses,
  formToBlockInput,
  formToMenuInput,
  formToPageInput,
  groupByPosition,
  isExternalURLOrEmpty,
  isIconNameOrEmpty,
  isLinkOrEmpty,
  menuLinkSummary,
  menuToForm,
  pagePath,
  pageToForm,
} from './utils'

const page: StaticPage = {
  id: 3,
  title: '用户协议',
  slug: 'terms',
  body: '<h2>第一条</h2><p>欢迎</p>',
  body_format: 'html',
  status: 'published',
  seo_title: '协议',
  seo_desc: '简介',
  is_footer: true,
  created_at: '2026-10-25T00:00:00Z',
  updated_at: '2026-10-25T01:00:00Z',
}

describe('静态页的表单转换', () => {
  it('编辑时回填已保存的值', () => {
    expect(pageToForm(page)).toEqual({
      title: '用户协议',
      slug: 'terms',
      body: '<h2>第一条</h2><p>欢迎</p>',
      status: 'published',
      seo_title: '协议',
      seo_desc: '简介',
      is_footer: true,
    })
  })
  it('新建时的默认值：草稿、不是页脚页', () => {
    expect(pageToForm()).toMatchObject({ status: 'draft', is_footer: false, slug: '', body: '' })
  })
  it('提交时去空白、slug 转小写；正文一律按富文本（rich）保存，老的 html 页面也统一成 rich', () => {
    const f = { ...pageToForm(page), title: '  用户协议  ', slug: '  Terms-2026 ', body: ' <p>x</p> ', seo_title: ' 协议 ', seo_desc: ' 简介 ' }
    expect(formToPageInput(f)).toEqual({
      title: '用户协议',
      slug: 'terms-2026',
      body: '<p>x</p>',
      body_format: 'rich',
      status: 'published',
      seo_title: '协议',
      seo_desc: '简介',
      is_footer: true,
    })
  })
})

describe('静态页的前台地址与摘要', () => {
  it('前台地址是 /p/<slug>', () => {
    expect(pagePath('terms')).toBe('/p/terms')
    expect(pagePath('  Terms  ')).toBe('/p/terms')
    expect(pagePath('   ')).toBe('')
  })
  it('摘要去掉标签并压缩空白', () => {
    expect(bodySummary('<h2>第一条</h2>\n<p>欢迎&nbsp;使用</p>')).toBe('第一条 欢迎 使用')
    expect(bodySummary('<p>' + '字'.repeat(80) + '</p>', 10)).toBe('字'.repeat(10) + '…')
    expect(bodySummary('')).toBe('')
  })
  it('能看出正文被后端清洗过（只比较去掉首尾空白后的文本）', () => {
    expect(bodyWasSanitized('<p>x</p><script>alert(1)</script>', '<p>x</p>')).toBe(true)
    expect(bodyWasSanitized('  <p>x</p>  ', '<p>x</p>')).toBe(false)
  })
})

const menu: SiteMenu = {
  id: 5,
  position: 'footer',
  label: '用户协议',
  link_type: 'page',
  link_value: 'terms',
  group_title: '帮助与条款',
  icon: 'file-text',
  tabbar_icon: '',
  badge: '新',
  visible_to: 'all',
  device: 'all',
  new_tab: false,
  status: 'active',
  sort: 1,
  created_at: '2026-10-25T00:00:00Z',
  updated_at: '2026-10-25T00:00:00Z',
  target_issue: '',
}

describe('前台菜单的表单转换', () => {
  it('编辑时回填，新建时按给定位置与默认值', () => {
    expect(menuToForm(menu)).toMatchObject({
      position: 'footer',
      label: '用户协议',
      link_type: 'page',
      link_value: 'terms',
      group_title: '帮助与条款',
    })
    expect(menuToForm(undefined, 'mobile_tabbar')).toMatchObject({
      position: 'mobile_tabbar',
      link_type: 'internal',
      link_value: 'home',
      visible_to: 'all',
      device: 'all',
      status: 'active',
      new_tab: false,
    })
  })
  it('slug 类链接转小写，站内页与外部网址保持原样', () => {
    expect(formToMenuInput({ ...menuToForm(menu), link_value: '  Terms-2026  ' }).link_value).toBe('terms-2026')
    expect(formToMenuInput({ ...menuToForm(menu), link_type: 'internal', link_value: 'home' }).link_value).toBe('home')
    expect(formToMenuInput({ ...menuToForm(menu), link_type: 'external', link_value: ' https://Example.com/A ' }).link_value).toBe(
      'https://Example.com/A'
    )
  })
  it('分组标题只有页脚保留，底栏图标只有手机底栏保留', () => {
    const f = { ...menuToForm(menu), group_title: '帮助', tabbar_icon: 'house' }
    expect(formToMenuInput({ ...f, position: 'top' })).toMatchObject({ group_title: '', tabbar_icon: '' })
    expect(formToMenuInput({ ...f, position: 'footer' })).toMatchObject({ group_title: '帮助', tabbar_icon: '' })
    expect(formToMenuInput({ ...f, position: 'mobile_tabbar' })).toMatchObject({ group_title: '', tabbar_icon: 'house' })
  })
  it('链接摘要：站内页显示中文名', () => {
    expect(menuLinkSummary({ link_type: 'internal', link_value: 'way_home' })).toBe('回家的路')
    expect(menuLinkSummary({ link_type: 'internal', link_value: 'unknown' })).toBe('unknown')
    expect(menuLinkSummary({ link_type: 'page', link_value: 'terms' })).toBe('静态页：terms')
    expect(menuLinkSummary({ link_type: 'external', link_value: 'https://example.com' })).toBe('https://example.com')
  })
  it('图标名与外部网址校验（与后端一致）', () => {
    for (const ok of ['', '  ', 'house', 'user-round', 'Grid3x3']) expect(isIconNameOrEmpty(ok)).toBe(true)
    for (const bad of ['房子', 'house!', 'a'.repeat(41)]) expect(isIconNameOrEmpty(bad)).toBe(false)
    for (const ok of ['', 'https://example.com', 'http://a.example.com/x?y=1']) expect(isExternalURLOrEmpty(ok)).toBe(true)
    for (const bad of ['/vip', 'javascript:alert(1)', 'example.com', 'https://user:pw@example.com/', 'https:// example.com']) {
      expect(isExternalURLOrEmpty(bad)).toBe(false)
    }
  })
  it('按位置分组并统计启用数', () => {
    const list = [menu, { ...menu, id: 6, position: 'top' as const }, { ...menu, id: 7, position: 'top' as const, status: 'inactive' as const }]
    const groups = groupByPosition(list)
    expect(groups.top.map((m) => m.id)).toEqual([6, 7])
    expect(groups.footer.map((m) => m.id)).toEqual([5])
    expect(groups.mobile_tabbar).toEqual([])
    expect(activeCount(groups.top)).toBe(1)
  })
})

describe('防丢地址的表单转换', () => {
  const addresses = {
    notice: '请收藏永久地址',
    latest_url: 'https://new.example.com',
    permanent_url: 'https://perm.example.com',
    backup_urls: ['https://b1.example.com'],
    publish_page: '',
    support_url: '',
    telegram: '',
    socials: [{ name: '微博', url: 'https://weibo.com/x' }],
  }
  it('列表包成对象数组（useFieldArray 用）', () => {
    expect(addressesToForm(addresses)).toMatchObject({
      backup_urls: [{ url: 'https://b1.example.com' }],
      socials: [{ name: '微博', url: 'https://weibo.com/x' }],
    })
    expect(addressesToForm()).toMatchObject({ notice: '', backup_urls: [], socials: [] })
  })
  it('提交时去空白并丢掉整行留空的备用地址与社交账号', () => {
    const f = addressesToForm(addresses)
    f.backup_urls.push({ url: '   ' })
    f.socials.push({ name: '  ', url: '  ' })
    f.notice = '  请收藏  '
    expect(formToAddresses(f)).toEqual({
      notice: '请收藏',
      latest_url: 'https://new.example.com',
      permanent_url: 'https://perm.example.com',
      backup_urls: ['https://b1.example.com'],
      publish_page: '',
      support_url: '',
      telegram: '',
      socials: [{ name: '微博', url: 'https://weibo.com/x' }],
    })
  })
})

// ---------- 页面区块 ----------

const block: Block = {
  id: 7,
  page: 'channel:city',
  type: 'poster_grid',
  title: '都市热播',
  subtitle: '每天更新',
  item_limit: 12,
  status: 'active',
  more_link: '/explore',
  cols_desktop: 5,
  device: 'mobile',
  source_type: 'auto',
  source: {
    drama_ids: [],
    topic_id: 0,
    term_codes: ['city'],
    tag_id: 20,
    sort: 'hot',
    window: '7d',
    rank_board: '',
    ad_slot: '',
    announce_kind: '',
    topic_ids: [],
    tag_ids: [],
  },
  custom_html: '',
  sort: 2,
  created_at: '2026-10-25T00:00:00Z',
  updated_at: '2026-10-25T01:00:00Z',
}

describe('区块的表单转换', () => {
  it('新建时带上默认值与当前页面', () => {
    const f = blockToForm(undefined, 'channel:city')
    expect(f).toMatchObject({
      page: 'channel:city',
      type: 'poster_grid',
      item_limit: '24',
      cols_desktop: '6',
      status: 'active',
      device: 'all',
      source_type: 'auto',
      sort: 'latest',
      window: 'all',
      announce_kind: 'topbar',
    })
    expect(f.drama_ids).toEqual([])
    expect(f.topic).toBeNull()
  })

  it('编辑时回填已保存的来源', () => {
    const f = blockToForm(block)
    expect(f).toMatchObject({ page: 'channel:city', item_limit: '12', cols_desktop: '5', sort: 'hot', window: '7d', term_codes: ['city'] })
    expect(f.tag).toEqual({ id: 20, label: '标签 #20' })
  })

  it('按类型裁剪来源：手选只留剧编号', () => {
    const f = blockToForm(block)
    const input = formToBlockInput({
      ...f,
      type: 'carousel',
      source_type: 'manual',
      drama_ids: [{ id: 3 }, { id: 1, title: '剧甲' }],
    })
    expect(input.source_type).toBe('manual')
    expect(input.source.drama_ids).toEqual([3, 1])
    // 自动来源的参数不会一起提交
    expect(input.source.term_codes).toEqual([])
    expect(input.source.tag_id).toBeNull()
    expect(input.source.sort).toBe('')
  })

  it('按类型裁剪来源：非剧类区块只留自己的参数，来源方式恒为 auto', () => {
    const f = blockToForm(block)
    const rank = formToBlockInput({ ...f, type: 'rank', source_type: 'manual', rank_board: 'drama_week', drama_ids: [{ id: 1 }] })
    expect(rank.source_type).toBe('auto')
    expect(rank.source.rank_board).toBe('drama_week')
    expect(rank.source.drama_ids).toEqual([])
    const tagCloud = formToBlockInput({ ...f, type: 'tag_cloud', tag_ids: [{ id: 21, label: '逆袭' }] })
    expect(tagCloud.source.tag_ids).toEqual([21])
    expect(tagCloud.source.rank_board).toBe('')
  })

  it('数量与每行个数转成数字，HTML 只在自定义 HTML 区块提交', () => {
    const f = blockToForm(block)
    const grid = formToBlockInput({ ...f, custom_html: '<p>x</p>' })
    expect(grid.item_limit).toBe(12)
    expect(grid.cols_desktop).toBe(5)
    expect(grid.custom_html).toBe('')
    const html = formToBlockInput({ ...f, type: 'custom_html', custom_html: '  <p>活动</p>  ' })
    expect(html.custom_html).toBe('<p>活动</p>')
  })

  it('专题来源提交专题编号', () => {
    const f = blockToForm(block)
    const input = formToBlockInput({ ...f, type: 'feature', source_type: 'topic', topic: { id: 10, label: '专题甲' } })
    expect(input.source.topic_id).toBe(10)
  })
})

describe('区块的列表显示', () => {
  it('来源摘要按类型给一句话', () => {
    expect(blockSourceSummary(block)).toBe('自动：最热 · 7 天内上新 · 取值 city · 标签 #20')
    expect(blockSourceSummary({ ...block, source_type: 'manual', source: { ...block.source, drama_ids: [3, 1] } })).toBe('手选 2 部：#3、#1')
    expect(blockSourceSummary({ ...block, source_type: 'topic', source: { ...block.source, topic_id: 10 } })).toBe('专题 #10')
    expect(blockSourceSummary({ ...block, type: 'rank', source: { ...block.source, rank_board: 'drama_month' } })).toBe('剧集月榜')
    expect(blockSourceSummary({ ...block, type: 'ad', source: { ...block.source, ad_slot: 'home_top' } })).toBe('广告位 home_top')
    expect(blockSourceSummary({ ...block, type: 'announce', source: { ...block.source, announce_kind: 'popup' } })).toBe('弹窗')
    expect(blockSourceSummary({ ...block, type: 'topic_group' })).toBe('列在专题列表页的专题')
    expect(blockSourceSummary({ ...block, type: 'tag_cloud', source: { ...block.source, tag_ids: [1, 2] } })).toBe('指定 2 个标签')
    expect(blockSourceSummary({ ...block, type: 'custom_html', custom_html: '<p>双十一 活动</p>' })).toBe('双十一 活动')
  })

  it('只有列表类区块才显示「最多 N 项」', () => {
    expect(blockHasList('poster_grid')).toBe(true)
    expect(blockHasList('rank')).toBe(true)
    expect(blockHasList('ad')).toBe(false)
    expect(blockHasList('custom_html')).toBe(false)
  })

  it('页面名取下拉里的显示名，找不到时用原值', () => {
    const pages = [
      { value: 'home', label: '首页' },
      { value: 'channel:city', label: '都市' },
    ]
    expect(blockPageLabel('channel:city', pages)).toBe('都市')
    expect(blockPageLabel('channel:gone', pages)).toBe('channel:gone')
  })

  it('查看全部链接收站内路径与 http(s) 地址', () => {
    expect(isLinkOrEmpty('')).toBe(true)
    expect(isLinkOrEmpty('/explore')).toBe(true)
    expect(isLinkOrEmpty('https://example.com/a')).toBe(true)
    expect(isLinkOrEmpty('javascript:alert(1)')).toBe(false)
    expect(isLinkOrEmpty('explore')).toBe(false)
    expect(isLinkOrEmpty('/ex plore')).toBe(false)
  })
})
