import { Boxes } from 'lucide-react'

/**
 * 内置页面登记表：页面编码 → 前端路由。与后端迁移 00003 / 00005 / 00010 / 00012 之后的 admin_menu 种子一一对应。
 * 侧栏的一级 / 二级结构、名称、图标、排序与显示都在后台「后台菜单」中配置（GET /api/admin/me/menus），
 * 这里只保留代码层面的信息：路由地址、默认名称（菜单被隐藏时占位页用），以及未实现模块的文档与任务编号。
 */
export type PageInfo = {
  code: string
  url: string
  title: string
  doc: string
  task: string
}

export const pages: PageInfo[] = [
  { code: 'dashboard', url: '/', title: '运营数据看板', doc: 'docs/06 第 1 节', task: 'A4-1' },
  { code: 'analytics_dramas', url: '/analytics/dramas', title: '剧集分析', doc: 'docs/06 第 2、3 节', task: 'A4-1' },
  { code: 'dramas', url: '/dramas', title: '剧集', doc: 'docs/06 第 4、5 节', task: 'A1-2' },
  { code: 'tags', url: '/tags', title: '标签', doc: 'docs/06 第 7 节', task: 'A1-3' },
  { code: 'import', url: '/import', title: '批量导入', doc: 'docs/06 第 8 节', task: 'A4-2' },
  { code: 'resource_checks', url: '/resource-checks', title: '资源体检', doc: 'docs/06 第 9 节', task: 'A4-2' },
  { code: 'resource_warm', url: '/resource-warm', title: '资源预热', doc: 'docs/06 第 10 节', task: 'A4-2' },
  { code: 'taxonomies', url: '/taxonomies', title: '筛选维度', doc: 'docs/06 第 11 节', task: 'A1-3' },
  { code: 'menus', url: '/menus', title: '前台菜单', doc: 'docs/06 第 12 节', task: 'A3-2' },
  { code: 'pages', url: '/pages', title: '静态页', doc: 'docs/06 第 13 节', task: 'A3-2' },
  { code: 'topics', url: '/topics', title: '专题', doc: 'docs/06 第 14 节', task: 'A1-3' },
  { code: 'blocks', url: '/blocks', title: '页面区块', doc: 'docs/06 第 15 节', task: 'A3-1' },
  { code: 'brand', url: '/brand', title: '品牌与主题', doc: 'docs/06 第 16 节', task: 'A3-3' },
  { code: 'seo', url: '/seo', title: 'SEO 与统计', doc: 'docs/06 第 17 节', task: 'A3-3' },
  { code: 'ads', url: '/ads', title: '广告位', doc: 'docs/06 第 18 节', task: 'A3-4' },
  { code: 'announcements', url: '/announcements', title: '公告', doc: 'docs/06 第 19 节', task: 'A3-2' },
  { code: 'ranks', url: '/ranks', title: '排行干预', doc: 'docs/06 第 21 节', task: 'A3-4' },
  { code: 'hotsearch', url: '/hotsearch', title: '热搜词', doc: 'docs/06 第 22 节', task: 'A3-4' },
  { code: 'finance', url: '/analytics/finance', title: '收入与成本', doc: 'docs/06 第 23 节', task: 'A4-1' },
  { code: 'vip_plans', url: '/vip/plans', title: '会员套餐', doc: 'docs/06 第 23a 节', task: 'S3-15 / A3-5' },
  { code: 'moderation', url: '/moderation', title: '评论与弹幕审核', doc: 'docs/06 第 24 节', task: 'A2-2' },
  { code: 'reports', url: '/reports', title: '举报队列', doc: 'docs/06 第 25 节', task: 'A2-2' },
  { code: 'users', url: '/users', title: '用户', doc: 'docs/06 第 26 节', task: 'A2-2' },
  { code: 'blockwords', url: '/blockwords', title: '屏蔽词', doc: 'docs/06 第 27 节', task: 'A2-2' },
  { code: 'emojis', url: '/emojis', title: '表情管理', doc: 'docs/06 表情/表情包管理', task: 'S2-5' },
  // 反馈社区（独立产品，文档在 feedback/docs；后端在 service/，表前缀 fb_，见 feedback/docs/adr/0008）
  { code: 'fb_projects', url: '/feedback/projects', title: '内测项目', doc: 'feedback/docs/07 第 8 节', task: 'FB-A1' },
  { code: 'fb_issues', url: '/feedback/issues', title: '反馈分诊', doc: 'feedback/docs/07 第 6 节', task: 'FB-A2' },
  { code: 'fb_posts', url: '/feedback/posts', title: '项目动态', doc: 'feedback/docs/07 第 9.1 节', task: 'FB-A3' },
  { code: 'fb_invites', url: '/feedback/invites', title: '邀请码', doc: 'feedback/docs/07 第 8.3 节', task: 'FB-A4' },
  { code: 'fb_users', url: '/feedback/users', title: '社区成员', doc: 'feedback/docs/07 第 10.3 节', task: 'FB-A5' },
  { code: 'fb_topics', url: '/feedback/topics', title: '议题区', doc: 'feedback/docs/08 第 8.5 节', task: 'FB-A6' },
  { code: 'fb_api_keys', url: '/feedback/api-keys', title: '开放接口', doc: 'feedback/docs/08 第 8.7 节', task: 'FB-A7' },
  { code: 'storage_providers', url: '/storage/providers', title: '存储源', doc: 'docs/12 第 8.1 节', task: 'A1-4a' },
  { code: 'storage_lines', url: '/storage/lines', title: '线路', doc: 'docs/12 第 8.2 节', task: 'A1-4a' },
  { code: 'storage_preview', url: '/storage/preview', title: '选线预览与健康', doc: 'docs/12 第 8.3 节', task: 'A1-4a' },
  { code: 'storage_settings', url: '/storage/settings', title: '存储设置', doc: 'docs/12 第 8.4 节', task: 'A1-4a' },
  { code: 'ftp', url: '/ftp', title: '资源机 FTP', doc: 'docs/06 第 31 节', task: 'A1-4' },
  { code: 'cache', url: '/cache', title: '缓存', doc: 'docs/06 第 28 节', task: 'A4-2' },
  { code: 'config', url: '/config', title: '配置导出导入', doc: 'docs/06 第 29 节', task: 'A4-2' },
  { code: 'apikeys', url: '/apikeys', title: 'API Key', doc: 'docs/06 第 32 节', task: 'A4-3' },
  { code: 'audit', url: '/audit', title: '审计日志', doc: 'docs/06 第 34 节', task: 'A4-3' },
  { code: 'trash', url: '/trash', title: '回收站', doc: 'docs/06 第 35 节', task: 'A4-3' },
  { code: 'features', url: '/features', title: '功能开关与年龄门', doc: 'docs/06 第 36 节', task: 'A3-3' },
  { code: 'domains', url: '/domains', title: '域名与证书', doc: 'docs/06 第 38 节', task: 'A4-4' },
  { code: 'security', url: '/security', title: '安全中心', doc: 'docs/06 第 39 节', task: 'A4-4' },
  { code: 'admins', url: '/admins', title: '管理员', doc: 'docs/06 第 33 节', task: 'A1-1a' },
  { code: 'roles', url: '/roles', title: '角色', doc: 'docs/06 第 33a 节', task: 'A1-1a' },
  { code: 'role_permissions', url: '/role-permissions', title: '角色权限', doc: 'docs/06 第 33b 节', task: 'A1-1a' },
  { code: 'admin_menus', url: '/admin-menus', title: '后台菜单', doc: 'docs/06 第 33c 节', task: 'A1-1a' },
]

/** 按路径查找内置页面。 */
export function findPageByPath(pathname: string): PageInfo | null {
  const normalized = pathname.replace(/\/+$/, '') || '/'
  return pages.find((p) => p.url === normalized) ?? null
}

export const BRAND = { name: '短剧运营后台', icon: Boxes }
