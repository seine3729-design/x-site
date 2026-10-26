#!/usr/bin/env node
/**
 * 本地开发用的种子数据。
 *
 * 全程走真实接口（后台 /api/admin/feedback/*、前台 /api/v1/feedback/*），
 * 所以编号分配、状态历史、贡献记账、计数字段都是后端真算出来的，
 * 不是直接塞库——用它造出来的数据和真实使用产生的数据没有区别。
 *
 * 可重复运行：每一步先查是否已存在，已存在就复用。
 *
 * 用法（仓库根目录已 ./dev.sh up）：
 *   node feedback/scripts/seed-dev-data.mjs
 *
 * 需要一个有 fb_* 权限的后台账号，默认读环境变量：
 *   SEED_ADMIN_USER（默认 fbe2e）、SEED_ADMIN_PASS
 */
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..', '..')

// ---------- 配置 ----------

const env = Object.fromEntries(
  readFileSync(join(root, '.env'), 'utf8')
    .split('\n')
    .filter((l) => l.trim() && !l.startsWith('#') && l.includes('='))
    .map((l) => {
      const i = l.indexOf('=')
      return [l.slice(0, i).trim(), l.slice(i + 1).trim()]
    }),
)

const BASE = `http://127.0.0.1:${env.SERVICE_PORT || 8080}`
const ADMIN_ORIGIN = `http://localhost:${env.ADMIN_PORT || 5173}`
const EDGE = env.EDGE_SHARED_SECRET
const SVC = env.BFF_SERVICE_TOKEN
const ADMIN_USER = process.env.SEED_ADMIN_USER || 'fbe2e'
const ADMIN_PASS = process.env.SEED_ADMIN_PASS || 'Fb-E2E-pass-2026!'
const USER_PASS = 'Dj66-seed-pass-2026!'

let cookie = ''
let csrf = ''

// ---------- HTTP ----------

async function call(method, path, { body, headers = {}, token } = {}) {
  const h = { accept: 'application/json', ...headers }
  if (body !== undefined) h['content-type'] = 'application/json'
  if (token) h.authorization = `Bearer ${token}`
  const res = await fetch(BASE + path, {
    method,
    headers: h,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  let json = null
  try {
    json = text ? JSON.parse(text) : null
  } catch {
    /* 非 JSON 响应（例如 403 空体）保持 null */
  }
  return { status: res.status, body: json, raw: text, headers: res.headers }
}

/** 后台调用：Origin + 回源密钥 + 会话 Cookie + CSRF。 */
async function admin(method, path, body) {
  const headers = { 'x-edge-secret': EDGE, origin: ADMIN_ORIGIN }
  if (cookie) headers.cookie = cookie
  if (method !== 'GET') headers['x-csrf-token'] = csrf
  const out = await call(method, path, { body, headers })
  if (out.status >= 400) {
    throw new Error(`${method} ${path} → ${out.status} ${out.raw.slice(0, 300)}`)
  }
  return out.body?.data
}

/** 前台调用：带服务令牌模拟 SSR 转发（命中明文豁免）。 */
async function front(method, path, { body, token } = {}) {
  const out = await call(method, path, { body, token, headers: { 'x-service-token': SVC } })
  return out
}

async function frontOk(method, path, opts) {
  const out = await front(method, path, opts)
  if (out.status >= 400) {
    throw new Error(`${method} ${path} → ${out.status} ${out.raw.slice(0, 300)}`)
  }
  return out.body?.data
}

// ---------- 种子内容 ----------

const PROJECTS = [
  {
    slug: 'dj66-app',
    key_prefix: 'APP',
    name: '短剧66 APP',
    summary: 'iOS / Android 客户端，内测重点是播放稳定性与离线下载。',
    description:
      '短剧66 的移动客户端。这一轮主要想知道两件事：切集和拖进度时播放器稳不稳，以及离线下载在地铁、电梯这种弱网环境下够不够用。\n\n遇到播放问题时，麻烦顺手记下当时的网络（WiFi / 蜂窝）和大概时间点，我们能对上服务端日志。',
    try_url: 'https://example.com/dj66-app',
    status: 'recruiting',
    visibility: 'public',
    join_mode: 'open',
    sort: 10,
  },
  {
    slug: 'dj66-web',
    key_prefix: 'WEB',
    name: '短剧66 网页版',
    summary: '浏览器端，重点是首屏速度、搜索结果和投屏。',
    description:
      '网页版这一轮在改搜索和首页推荐。想请大家重点试：搜剧名和搜演员能不能找到想要的，以及投屏到电视的成功率。',
    try_url: 'https://example.com/dj66-web',
    status: 'active',
    visibility: 'public',
    join_mode: 'open',
    sort: 20,
  },
  {
    slug: 'dj66-creator',
    key_prefix: 'CRT',
    name: '创作者中心',
    summary: '给内容方用的上传与数据后台，仅邀请参与。',
    description:
      '创作者中心面向签约的内容方，功能包括剧集上传、分集排期、播放数据。因为涉及未公开的内容，这个项目只对受邀成员开放。',
    status: 'active',
    visibility: 'members',
    join_mode: 'invite_only',
    sort: 30,
  },
  {
    slug: 'dj66-tv',
    key_prefix: 'TV',
    name: '短剧66 TV 版',
    summary: '电视端，刚开始招募，先收遥控器操作上的问题。',
    description:
      'TV 版还在很早的阶段。现在最想知道的是：遥控器上下左右能不能顺畅地走完「找剧 → 播放 → 切集」这条路。',
    status: 'recruiting',
    visibility: 'public',
    // 用 application（需审核）：申请流程已经实现，join 会落成 pending，
    // 由运营在后台批准成 active。joinProjects 会刻意留几个人停在 pending，
    // 这样前台「申请中，等待确认」那个状态有真实数据可看。
    join_mode: 'application',
    sort: 40,
  },
]

/**
 * 社区账号。
 *
 * `invite` 指用哪个项目的邀请码注册：邀请码带项目时，注册会自动把人加进该项目。
 * 创作者中心是「仅邀请」项目，而后端目前没有「把已有账号加进项目」的能力
 * （后台的改角色接口要求成员已存在），所以内容方用自己的邀请码单独注册——
 * 这也更接近真实情况：内容方和普通内测用户本来就不是同一批人。
 */
const USERS = [
  // 前 6 个是「主力内测用户」：手写的那批反馈都署名在他们身上，
  // 贡献榜前几名也是他们，页面上看起来才像有活人在用。
  { handle: 'xiaoyu', nickname: '小鱼', invite: 'dj66-app' },
  { handle: 'azhe', nickname: '阿哲', invite: 'dj66-app' },
  { handle: 'mingming', nickname: '明明', invite: 'dj66-app' },
  { handle: 'lanlan', nickname: '兰兰', invite: 'dj66-app' },
  { handle: 'dapeng', nickname: '大鹏', invite: 'dj66-app' },
  { handle: 'qiqi', nickname: '柒柒', invite: 'dj66-app' },

  // 这一批撑数据量：让贡献榜有真实的长尾排名、成员列表需要翻页、
  // 反馈的署名不会总是那六个人。
  { handle: 'xiaowen', nickname: '小温', invite: 'dj66-app' },
  { handle: 'laozhang', nickname: '老张', invite: 'dj66-app' },
  { handle: 'nannan', nickname: '楠楠', invite: 'dj66-app' },
  { handle: 'ayuan', nickname: '阿远', invite: 'dj66-app' },
  { handle: 'xiaomai', nickname: '小麦', invite: 'dj66-app' },
  { handle: 'tiantian', nickname: '甜甜', invite: 'dj66-app' },
  { handle: 'kaige', nickname: '凯哥', invite: 'dj66-app' },
  { handle: 'yuyu', nickname: '鱼鱼', invite: 'dj66-app' },
  { handle: 'zhoubo', nickname: '周波', invite: 'dj66-app' },
  { handle: 'linxi', nickname: '林溪', invite: 'dj66-app' },
  { handle: 'hehe', nickname: '禾禾', invite: 'dj66-app' },
  { handle: 'dongdong', nickname: '东东', invite: 'dj66-app' },
  { handle: 'shuangshuang', nickname: '双双', invite: 'dj66-app' },
  { handle: 'guoguo', nickname: '果果', invite: 'dj66-app' },
  // 昵称留空：前台应该回退显示用户名，这条就是用来验证那个回退的
  { handle: 'noname_user', nickname: '', invite: 'dj66-app' },
  // 长昵称（接近 40 字上限）：检查列表与署名处的截断
  { handle: 'longnick', nickname: '一个特别长的昵称用来测试截断表现到底够不够', invite: 'dj66-app' },

  // 内容方：只在创作者中心活动
  { handle: 'studiolin', nickname: '林记工作室', invite: 'dj66-creator' },
  { handle: 'studiohe', nickname: '和光制作', invite: 'dj66-creator' },
  { handle: 'studioqing', nickname: '青禾影业', invite: 'dj66-creator' },
  { handle: 'studiomu', nickname: '木星短剧', invite: 'dj66-creator' },
]

const POSTS = [
  {
    project: 'dj66-app',
    kind: 'test_task',
    title: '本轮重点：离线下载 + 断网续播',
    body: '这一轮请大家重点试两条路径：\n\n1. 在 WiFi 下载 3 集，然后开飞行模式，看能不能正常从上次的位置接着播；\n2. 下载过程中切到后台、锁屏，回来看进度有没有停。\n\n下载失败时麻烦截一张「我的下载」页面，带上剧名和集数。',
    status: 'published',
    visibility: 'public',
    pinned: true,
  },
  {
    project: 'dj66-app',
    kind: 'changelog',
    title: '2.4.0 已上线：修掉了切集黑屏',
    body: '这个版本处理了 9 条内测反馈，其中最多人遇到的是切集时黑屏 2–3 秒。\n\n如果你之前提过相关反馈，可以更新后再试一次，仍有问题请在原反馈下补充。',
    status: 'published',
    visibility: 'public',
    pinned: false,
  },
  {
    project: 'dj66-app',
    kind: 'announcement',
    title: '内测名额扩到 500 人',
    body: '新增 200 个名额。邀请码可以转给身边常看短剧的朋友，但请不要公开发布到社交平台。',
    status: 'published',
    visibility: 'public',
    pinned: false,
  },
  {
    project: 'dj66-web',
    kind: 'test_task',
    title: '新版搜索上线，请试试搜演员',
    body: '搜索这一轮加了演员维度。想请大家试：\n\n- 只记得演员名字，能不能找到剧；\n- 剧名记错一两个字，还能不能搜到。\n\n搜不到的时候，把你输入的词原样贴给我们最有用。',
    status: 'published',
    visibility: 'public',
    pinned: true,
  },
  {
    project: 'dj66-web',
    kind: 'changelog',
    title: '首屏加载慢的问题改了一版',
    body: '首页海报改成了按需加载，首屏请求数从 40 多降到 12。慢网络下应该会明显一些。',
    status: 'published',
    visibility: 'public',
    pinned: false,
  },
  {
    project: 'dj66-creator',
    kind: 'test_task',
    title: '分集排期支持批量设置了',
    body: '现在可以一次选多集设置上线时间。请试一下跨天排期会不会错位。',
    status: 'published',
    visibility: 'members',
    pinned: false,
  },
  {
    project: 'dj66-tv',
    kind: 'announcement',
    title: 'TV 版开始招募内测用户',
    body: '支持的设备：小米电视、天猫魔盒、Apple TV。申请后我们会人工确认设备型号再开通。',
    status: 'published',
    visibility: 'public',
    pinned: true,
  },

  /*
   * 下面这些是后补的。
   *
   * 补的原因不是「数量不够」，而是**每个项目都必须有一条 test_task**：
   * 项目详情页顶部那块「这轮请重点测什么」只认 test_task 类型，
   * 没有就是一片空状态。dj66-tv 原来只有一条 announcement，页面上就那么空着。
   */
  {
    project: 'dj66-tv',
    kind: 'test_task',
    title: '本轮重点：遥控器走完「找剧 → 播放 → 切集」',
    body:
      '拿起遥控器，只用方向键和确认键，完成这三步：\n\n' +
      '1. 在首页找到一部你想看的剧\n' +
      '2. 进去播第一集\n' +
      '3. 切到第二集\n\n' +
      '中间任何一步让你停下来想「这个键是哪个」，就请提一条反馈，' +
      '写清楚你当时按了什么、期望发生什么。焦点框看不清、按了没反应、' +
      '退出后回不到原位置，这几类都要。',
    status: 'published',
    visibility: 'public',
    pinned: true,
  },
  {
    project: 'dj66-tv',
    kind: 'changelog',
    title: 'TV 0.3.0：焦点框加粗，切集不再闪黑',
    body:
      '这版主要修了两件被提得最多的事：\n\n' +
      '- 焦点框在浅色封面上看不清（TV-2、TV-9 等 6 条反馈）\n' +
      '- 切集时会闪一下黑屏（TV-5）\n\n' +
      '提过这两条的同学可以再试一下，还有问题请在原反馈下补充。',
    status: 'published',
    visibility: 'public',
    pinned: false,
  },
  {
    project: 'dj66-app',
    kind: 'test_task',
    title: '想请大家试试新的追剧提醒',
    body:
      '追剧提醒改成了可以自己设时间。想知道两件事：\n\n' +
      '1. 默认时间（每天 20:00）合不合适\n' +
      '2. 同时追五部以上的时候，提醒会不会太吵\n\n' +
      '如果你把提醒关掉了，也请说一声为什么关 —— 这个信息对我们同样有用。',
    status: 'published',
    visibility: 'public',
    pinned: false,
  },
  {
    project: 'dj66-web',
    kind: 'announcement',
    title: '网页版下周会有一次短暂维护',
    body: '预计周三凌晨 2:00–2:30，期间可能无法登录。已提交的反馈不受影响。',
    status: 'published',
    visibility: 'public',
    pinned: false,
  },
  {
    project: 'dj66-web',
    kind: 'test_task',
    title: '这轮看一下分类筛选好不好用',
    body:
      '分类页加了多选筛选。请试着用它找一部「去年的、悬疑的、已完结的」剧，\n' +
      '看看中间有没有哪一步让你猜不到该点哪里。',
    status: 'published',
    visibility: 'public',
    pinned: false,
  },
  {
    project: 'dj66-creator',
    kind: 'changelog',
    title: '创作者中心 1.2：排期支持复制上一集',
    body: '新增「复制上一集设置」，跨天排期的错位问题也一并修了。',
    status: 'published',
    visibility: 'members',
    pinned: false,
  },
  // 注意：这里只能写 PROJECTS 里定义的项目。
  // 库里还有一个 fbe2e-demo，那是早期端到端测试建的，不由本脚本管理，
  // projects.get('fbe2e-demo') 会是 undefined。
]

/**
 * 反馈。status 是最终想要的状态，脚本会用后台 triage 一步步合法流转过去。
 * 这样状态时间线是真实生成的，不是直接改字段。
 */
const FEEDBACK = [
  {
    project: 'dj66-app',
    author: 'xiaoyu',
    kind: 'bug',
    title: '切集时偶尔黑屏 2 到 3 秒',
    body: '连续看同一部剧，点「下一集」之后画面黑着，声音也没有，大概 2 到 3 秒才出来。\n\n不是每次都有，大概三四次里有一次。WiFi 和 4G 都遇到过。',
    env: 'iPhone 15 / iOS 19.2 / 短剧66 2.3.1 / WiFi',
    status: 'shipped',
    votes: ['azhe', 'mingming', 'lanlan', 'dapeng'],
    note: '定位到是切集时没有复用播放器实例，2.4.0 已修。',
  },
  {
    project: 'dj66-app',
    author: 'azhe',
    kind: 'bug',
    title: '下载完成后播放提示「文件损坏」',
    body: '下载了 5 集，前 3 集能看，第 4 集点开提示文件损坏，要重新下。重下之后还是一样。',
    env: 'Xiaomi 14 / Android 16 / 2.3.1 / 家里 WiFi',
    status: 'in_progress',
    votes: ['xiaoyu', 'qiqi'],
    note: '已经能稳定复现，正在查分片合并的逻辑。',
  },
  {
    project: 'dj66-app',
    author: 'mingming',
    kind: 'access',
    title: '晚上八点多经常加载不出来首页',
    body: '白天都正常，晚上八点到九点之间打开 APP，首页一直转圈，等十几秒会提示网络错误。退出重进有时候就好了。',
    env: 'iPhone 13 / iOS 18.6 / 2.3.1 / 中国移动 5G',
    status: 'verifying',
    votes: ['lanlan', 'dapeng', 'qiqi', 'azhe', 'xiaoyu'],
    note: '扩了一组接口实例，晚高峰的超时率已经降下来了，想请遇到过的朋友再试一晚。',
  },
  {
    project: 'dj66-app',
    author: 'lanlan',
    kind: 'experience',
    title: '倍速按钮太小，经常点成全屏',
    body: '播放页右下角那几个按钮挨得太近了，想点 1.5 倍速结果点到全屏，一只手拿手机的时候更容易点错。',
    env: 'iPhone 15 Pro Max / 2.3.1',
    status: 'planned',
    votes: ['xiaoyu', 'mingming', 'qiqi'],
    note: '已经排进 2.5.0，会把这一排按钮的间距和热区都放大。',
  },
  {
    project: 'dj66-app',
    author: 'dapeng',
    kind: 'feature',
    title: '希望支持画中画，想边看边聊天',
    body: '现在切出去 APP 就停了。希望能像视频网站那样缩成小窗浮在上面。',
    env: 'Android 16',
    status: 'triaged',
    votes: ['azhe', 'lanlan', 'qiqi', 'mingming'],
    note: '收到，这个需求提的人不少，正在评估不同系统上的实现成本。',
  },
  {
    project: 'dj66-app',
    author: 'qiqi',
    kind: 'feature',
    title: '想要追剧更新提醒',
    body: '在追的剧更新了没有任何提示，每天都得自己点进去看。希望能更新了推一条通知。',
    env: 'iPhone 14 / iOS 19.1',
    status: 'planned',
    votes: ['xiaoyu', 'azhe', 'lanlan', 'dapeng', 'mingming'],
    note: '已进入计划。先做站内提醒，系统推送要等通知权限的方案定下来。',
  },
  {
    project: 'dj66-app',
    author: 'xiaoyu',
    kind: 'bug',
    title: '锁屏后继续播，回到 APP 进度对不上',
    body: '锁屏的时候声音还在放，解锁回来画面是从锁屏那一刻接着播的，和声音差了十几秒。',
    env: 'iPhone 15 / iOS 19.2 / 2.3.1',
    status: 'submitted',
    votes: ['mingming'],
  },
  {
    project: 'dj66-app',
    author: 'azhe',
    kind: 'experience',
    title: '找不到「我的追剧」在哪',
    body: '第一次用的时候在首页找了半天没找到追剧列表，后来才发现在「我的」里面。希望首页就能看到。',
    env: 'Android 16',
    status: 'submitted',
  },
  {
    project: 'dj66-app',
    author: 'mingming',
    kind: 'competitor',
    title: '别家有一键跳过片头，挺好用',
    body: '在另一个短剧 App 上看到的：播到片头会浮出来一个「跳过片头」，点一下直接到正片，而且它会记住，下一集自动跳。',
    env: 'iPhone 13',
    status: 'triaged',
    votes: ['lanlan', 'qiqi'],
    note: '这个值得做，已经记下来排期。',
  },
  {
    project: 'dj66-app',
    author: 'lanlan',
    kind: 'content',
    title: '想看更多古装悬疑的剧',
    body: '现在古装多是甜宠，悬疑的几乎没有。希望多上一些。',
    status: 'declined',
    note: '内容排期由版权和制作周期决定，这一季确实没有古装悬疑的片源。等有了会在动态里说。',
  },
  {
    project: 'dj66-app',
    author: 'dapeng',
    kind: 'bug',
    title: '切集偶尔黑屏（和 APP-1 一样）',
    body: '我也遇到了黑屏，看了一下已经有人提过，补一条我的设备信息。',
    env: 'Pixel 9 / Android 16 / 2.3.1',
    status: 'duplicate',
    duplicateOfTitle: '切集时偶尔黑屏 2 到 3 秒',
    note: '与 APP-1 是同一个问题，已合并过去，请关注那一条的进展。',
  },
  {
    project: 'dj66-web',
    author: 'xiaoyu',
    kind: 'bug',
    title: '搜索结果里同一部剧出现两次',
    body: '搜「替身」出来两条一样的，点进去是同一部。',
    env: 'Chrome 146 / macOS 26.1',
    status: 'shipped',
    votes: ['azhe'],
    note: '是新旧两套索引都返回了结果，已经去重，上线了。',
  },
  {
    project: 'dj66-web',
    author: 'azhe',
    kind: 'access',
    title: '投屏到小米电视连不上',
    body: '点投屏能搜到电视，选了之后一直显示「连接中」，大概半分钟后失败。手机上的其他视频 App 投屏是正常的。',
    env: 'Chrome 146 / 小米电视 ES55 / 同一个 WiFi',
    status: 'in_progress',
    votes: ['mingming', 'dapeng'],
    note: '拿到同型号电视复现了，正在查 DLNA 的握手。',
  },
  {
    project: 'dj66-web',
    author: 'mingming',
    kind: 'experience',
    title: '首页推荐里已经看完的剧还在推',
    body: '追完的剧第二天还在首页第一排，有点占位置。希望看完的能往后排。',
    env: 'Chrome 146 / Windows',
    status: 'triaged',
    votes: ['lanlan', 'xiaoyu', 'qiqi'],
    note: '确认了，推荐没有用观看完成度做降权，已经记进待办。',
  },
  {
    project: 'dj66-web',
    author: 'lanlan',
    kind: 'feature',
    title: '希望能按演员筛选',
    body: '找某个演员演过的剧现在只能靠搜索，希望详情页点演员名字能看到他的全部剧。',
    status: 'planned',
    votes: ['qiqi', 'azhe'],
    note: '演员维度的数据已经有了，页面排在下个版本。',
  },
  {
    project: 'dj66-web',
    author: 'dapeng',
    kind: 'bug',
    title: '进度条拖到最后会直接跳下一集',
    body: '想看结尾的一小段，拖到最右边松手，直接跳到下一集了，退回来还得重新找。',
    env: 'Safari 26 / macOS 26.1',
    status: 'submitted',
    votes: ['xiaoyu'],
  },
  {
    project: 'dj66-web',
    author: 'qiqi',
    kind: 'experience',
    title: '手机浏览器上字幕偏小',
    body: '用手机打开网页版看，字幕比 APP 里小不少，晚上看有点费眼。希望能调字幕大小。',
    env: 'Safari / iPhone 14',
    status: 'submitted',
  },
  {
    project: 'dj66-web',
    author: 'xiaoyu',
    kind: 'access',
    title: '4G 下一直转圈，WiFi 正常',
    body: '同一部剧，家里 WiFi 秒开，出门用 4G 就一直加载。其他网站正常。',
    env: 'Chrome / Android 16 / 中国联通 4G',
    status: 'verifying',
    votes: ['mingming', 'azhe'],
    note: '给移动网络换了更低码率的默认清晰度，想请在 4G 下再试试。',
  },
  {
    project: 'dj66-creator',
    author: 'studiolin',
    kind: 'bug',
    title: '批量排期跨天会错位一小时',
    body: '选了 5 集设置每天 20:00 上线，保存后看列表，有两集变成了 19:00。',
    env: 'Chrome 146 / 创作者中心',
    status: 'in_progress',
    visibility: 'members',
    note: '是时区换算的问题，正在修。',
  },
  {
    project: 'dj66-creator',
    author: 'studiohe',
    kind: 'feature',
    title: '上传时希望能看到转码进度',
    body: '现在上传完就只有一个「处理中」，不知道还要等多久。希望有百分比。',
    status: 'triaged',
    visibility: 'members',
    note: '转码进度后端已经有了，页面补上就行。',
  },
  {
    project: 'dj66-tv',
    author: 'dapeng',
    kind: 'experience',
    title: '遥控器左右键在剧集列表里会跳格',
    body: '按一下右键，焦点跳过了两个，得退回来。上下键正常。',
    env: '小米电视 ES55 / TV 版 0.9.2',
    status: 'triaged',
    votes: ['qiqi'],
    note: '复现了，是列表的焦点步进算错了。',
  },
  {
    project: 'dj66-tv',
    author: 'qiqi',
    kind: 'bug',
    title: '返回键会直接退出 APP',
    body: '在播放页按返回，期望回到剧集列表，结果整个 APP 退出了。',
    env: '天猫魔盒 / TV 版 0.9.2',
    status: 'submitted',
    votes: ['dapeng'],
  },
  {
    project: 'dj66-app',
    author: 'mingming',
    kind: 'other',
    title: '想问一下内测反馈会不会公开',
    body: '我提的反馈里写了设备型号，不知道别人能不能看到。',
    status: 'closed',
    visibility: 'private',
    note: '可见性由你自己选：公开的所有人可见，私密的只有你和项目团队能看到。你这条是私密的。',
  },
  {
    project: 'dj66-app',
    author: 'xiaoyu',
    kind: 'feature',
    title: '希望能记住我的倍速设置',
    body: '我习惯 1.5 倍速，但每集都要重新点一次。',
    status: 'shipped',
    votes: ['azhe', 'lanlan', 'dapeng'],
    note: '2.4.0 起会记住上次的倍速。',
  },
  {
    project: 'dj66-web',
    author: 'azhe',
    kind: 'content',
    title: '《替身新娘》缺了第 18 到 20 集',
    body: '列表里显示 24 集，但 18 到 20 点进去是「暂无资源」。',
    status: 'shipped',
    votes: ['mingming'],
    note: '是这三集的转码失败了没重试，已经补上。',
  },
]

const TOPICS = [
  {
    project: 'dj66-app',
    title: '你希望下一版先做哪个功能？',
    body: '我们手上有三件事想做，但一个版本只来得及做一到两件：\n\n1. 画中画（切出去也能继续看）\n2. 追剧更新提醒\n3. 一键跳过片头\n\n想听听你平时是怎么看剧的——比如你是通勤路上看，还是躺着看？这会直接影响我们先做哪个。',
    answer_hint: '说说你平时看剧的场景，以及最想先要哪个',
    status: 'open',
    visibility: 'public',
    pinned: true,
    answers: [
      {
        author: 'xiaoyu',
        body: '我主要在地铁上看，所以最想要离线下载更稳一点，其次是跳过片头。画中画对我没用，通勤的时候也不会一边聊天一边看。',
        helpful: ['azhe', 'mingming', 'lanlan'],
        featured: true,
      },
      {
        author: 'azhe',
        body: '追剧提醒。我同时追四五部，经常忘了哪部更新了，每天挨个点进去看很烦。',
        helpful: ['qiqi', 'dapeng'],
      },
      {
        author: 'lanlan',
        body: '跳过片头 +1。短剧的片头本来就短，但每集都看一遍还是烦。如果能记住选择就更好了。',
        helpful: ['xiaoyu'],
      },
      {
        author: 'dapeng',
        body: '画中画。我一般是在电脑旁边放着当背景，偶尔抬头看一眼，现在切窗口就停了很不方便。',
        helpful: [],
      },
    ],
  },
  {
    project: 'dj66-app',
    title: '追剧提醒你更希望怎么收到？',
    body: '如果我们做更新提醒，你希望是哪种？\n\n- 系统推送（需要你开通知权限）\n- 只在 APP 里的小红点\n- 每天固定时间汇总一条\n\n也想知道：你能接受一天收到几条？',
    answer_hint: '说说你能接受的频率',
    status: 'open',
    visibility: 'public',
    pinned: false,
    answers: [
      {
        author: 'qiqi',
        body: '汇总一条就够了，一天来五六条推送我会直接关掉通知。晚上八点左右推最合适。',
        helpful: ['xiaoyu', 'mingming', 'azhe', 'lanlan'],
        featured: true,
      },
      {
        author: 'mingming',
        body: '我不想开系统推送，小红点就行。进 APP 的时候能看到哪几部更新了就够。',
        helpful: ['dapeng'],
      },
    ],
  },
  {
    project: 'dj66-web',
    title: '播放器改版两周了，你习惯了吗？',
    body: '上个月我们把网页版播放器的控制条从底部移到了右侧。当时的想法是竖屏看的时候底部容易误触。\n\n用了两周，想听真实感受：是习惯了，还是一直别扭？',
    answer_hint: '直说就行，不用客气',
    status: 'closed',
    visibility: 'public',
    pinned: false,
    answers: [
      {
        author: 'azhe',
        body: '一开始很别扭，大概三天就习惯了。现在觉得比之前好，底部那个位置我以前真的老点错。',
        helpful: ['mingming', 'lanlan'],
        featured: true,
      },
      {
        author: 'lanlan',
        body: '还是不太习惯。我用鼠标的时候手一直在下面，要往右上角跑一趟。如果能让我自己选位置就好了。',
        helpful: ['xiaoyu', 'dapeng', 'qiqi'],
      },
      {
        author: 'xiaoyu',
        body: '手机上横屏看挺好的，右侧不挡字幕。电脑上我没什么感觉。',
        helpful: [],
      },
    ],
  },

  /*
   * 创作者中心的议题。
   *
   * 必须手写在这里，不能指望批量议题分配到它：批量议题靠标题判重、标题里不带
   * 项目标识，所以在跑过一次的库上不会重新分布（见下面 buildBulkTopics 的说明）。
   * 而这个项目的详情页如果没有议题，就是一块空状态。
   *
   * visibility 用 members：创作者中心本身就是仅成员可见的项目，
   * 议题跟着项目走才合理，顺便也覆盖了「仅成员可见议题」这条路径。
   */
  {
    project: 'dj66-creator',
    title: '排期功能你们最希望先补哪一块？',
    body:
      '创作者中心的排期现在只能按集设置上线时间。几个方向想听听你们的意见：\n\n' +
      '1. 批量改期（选中多集一起往后推几天）\n' +
      '2. 排期模板（比如「每周一三五 20:00」存成模板复用）\n' +
      '3. 冲突提醒（同一天排了两部剧时提示一下）\n\n' +
      '你们平时是提前几天排的？一次排几集？这决定我们先做哪个。',
    answer_hint: '说说你们的排期习惯，以及最想先要哪一块',
    status: 'open',
    visibility: 'members',
    pinned: true,
    answers: [
      {
        author: 'studiolin',
        body:
          '我们一般提前两周排，一次排十几集。最需要批量改期——' +
          '平台那边调档的时候，现在要一集一集改，十几集改下来很容易漏。',
        helpful: ['studiohe', 'studioqing'],
        featured: true,
      },
      {
        author: 'studiohe',
        body: '模板对我们更有用。我们的剧基本都是固定节奏，每次都重新设一遍挺浪费时间。',
        helpful: ['studiomu'],
      },
      {
        author: 'studioqing',
        body: '冲突提醒。上个月我们两部剧撞了同一天首播，是上线之后才发现的。',
        helpful: ['studiolin', 'studiohe'],
      },
    ],
  },
]

/**
 * 批量反馈。
 *
 * 上面那 26 条是手写的真实内容，用来看页面长什么样；这里再按模板生成一批，
 * 目的只有一个：让分页、筛选、排序有足够的数据可测。
 * 条数由 SEED_BULK 控制（默认 60，传 0 跳过）。
 */
const BULK_TEMPLATES = [
  { kind: 'bug', title: '{what}在{where}会{how}', what: ['进度条', '弹幕', '字幕', '音量条', '剧集列表', '搜索框', '收藏按钮', '分享面板', '倍速按钮', '投屏图标', '播放器手势', '底部导航'], where: ['横屏下', '弱网下', '切后台回来后', '连续看三集后', '首次打开时', '低电量模式下', '插耳机之后', '来电话挂断后', '锁屏再解锁后', '分屏模式下'], how: ['不响应', '显示错位', '闪一下就没了', '卡住要等几秒', '数字不对', '重复出现两次', '位置偏了一截', '点了没反应要再点一次'] },
  { kind: 'bug', title: '{scene}时{what}会{how}', scene: ['快速连点下一集', '拖动进度条到最后', '同时打开两个剧', '从通知栏点进来'], what: ['播放器', '剧集列表', '追剧状态', '观看记录'], how: ['白屏', '退回首页', '状态不同步', '多出一条重复记录'] },
  { kind: 'experience', title: '{what}的{aspect}不太好用', what: ['倍速菜单', '清晰度切换', '追剧入口', '历史记录', '评论区', '搜索建议', '分类筛选', '个人中心', '合集页', '消息提醒'], aspect: ['位置', '层级', '文案', '触控区域', '反馈提示', '默认值', '排序规则', '加载顺序'] },
  { kind: 'experience', title: '{what}希望能{better}', what: ['首页信息密度', '列表封面比例', '夜间模式配色', '字幕字号'], better: ['再紧凑一点', '再克制一点', '可以自己调', '跟系统设置联动'] },
  { kind: 'feature', title: '希望能{action}', action: ['按更新时间排序追剧列表', '批量取消追剧', '把剧集加到自定义合集', '在列表里预览第一集', '导出我的观看记录', '设置每集自动跳过片尾', '锁定横屏方向', '夜间自动降低亮度', '给剧集打自己的标签', '设置追更提醒时间', '按演员筛选', '把弹幕关到只剩字幕'] },
  { kind: 'content', title: '想要更多{genre}的内容', genre: ['都市职场', '乡村逆袭', '悬疑推理', '年代情感', '科幻脑洞', '轻喜剧', '古装权谋', '医疗行业', '校园青春', '美食治愈'] },
  { kind: 'content', title: '{genre}这一类{issue}', genre: ['悬疑推理', '年代情感', '科幻脑洞', '古装权谋'], issue: ['更新太慢', '结局普遍仓促', '标签分得太粗', '推荐总是重复那几部'] },
  { kind: 'access', title: '{network}下{what}', network: ['地铁里', '电梯里', '公司网络', '校园网', '国际漫游', '高铁上', '酒店 WiFi', '移动热点'], what: ['完全打不开', '加载到一半停住', '只有声音没有画面', '登录一直失败', '反复提示网络异常', '首帧要等十几秒'] },
  { kind: 'competitor', title: '别家的{what}做得更顺', what: ['片头跳过', '追剧提醒', '投屏', '搜索联想', '合集推荐', '离线下载管理', '观看进度同步', '倍速记忆', '竖屏切横屏'] },
  { kind: 'other', title: '关于{topic}的一点想法', topic: ['内测反馈的处理节奏', '版本更新说明的写法', '社区里的称呼方式', '贡献值的计算口径'] },
]

const BULK_STATUS = [
  'submitted', 'submitted', 'submitted', 'submitted',
  'triaged', 'triaged', 'triaged',
  'planned', 'planned',
  'in_progress',
  'verifying',
  'shipped', 'shipped',
  'closed',
  'declined',
]

/** 固定种子的伪随机：每次跑生成同一批数据，才能保证脚本可重复运行。 */
function makeRandom(seed) {
  let state = seed
  return () => {
    state = (state * 1103515245 + 12345) & 0x7fffffff
    return state / 0x7fffffff
  }
}

/**
 * 按模板生成批量反馈。
 *
 * `activeByProject` 是「该项目当前的 active 成员」，由 joinProjects 回查得到。
 * 作者必须从里面挑：非成员提交会被后端拒掉，而那种失败混在几百条循环里极难定位
 * （以前这里写死了 dj66-app 的成员名单，dj66-tv 改成需审核之后就埋了这个坑）。
 */
function buildBulk(count, activeByProject) {
  const rand = makeRandom(20261024)
  const pick = (arr) => arr[Math.floor(rand() * arr.length)]
  // 权重：APP 最多，TV 最少，和真实项目的活跃度一致
  const weighted = ['dj66-app', 'dj66-app', 'dj66-app', 'dj66-web', 'dj66-web', 'dj66-tv']
  const projects = weighted.filter((slug) => (activeByProject.get(slug) ?? []).length > 0)
  if (projects.length === 0) throw new Error('没有任何项目有 active 成员，无法生成批量反馈')

  const out = []
  for (let i = 1; i <= count; i += 1) {
    const tpl = pick(BULK_TEMPLATES)
    let title = tpl.title
    for (const [key, values] of Object.entries(tpl)) {
      if (key === 'kind' || key === 'title') continue
      title = title.replace(`{${key}}`, pick(values))
    }
    const project = pick(projects)
    const members = activeByProject.get(project)
    // 投票也只能是该项目的成员
    const voters = members.filter(() => rand() < 0.3)
    out.push({
      project,
      author: pick(members),
      kind: tpl.kind,
      // 带序号避免标题重复：脚本靠标题判断是否已经造过
      title: `${title}（#${String(i).padStart(3, '0')}）`,
      body:
        `这是用于测试列表与筛选的样例反馈。\n\n现象：${title}。\n` +
        `复现步骤：打开 APP → 进入播放页 → 重复操作三到五次。\n` +
        `出现频率：大约 ${Math.ceil(rand() * 5)} 次里有 1 次。`,
      env: pick([
        'iPhone 15 / iOS 19.2 / WiFi',
        'iPhone 13 mini / iOS 18.6 / 4G',
        'Xiaomi 14 / Android 16 / 5G',
        'HUAWEI Mate 60 / HarmonyOS 5 / WiFi',
        'vivo X100 / Android 15 / 5G',
        'Chrome 146 / macOS 26.1',
        'Edge 146 / Windows 11',
        'Safari / iPad Air / WiFi',
        '小米电视 ES55',
        '天猫魔盒 6 / 千兆有线',
      ]),
      status: pick(BULK_STATUS),
      votes: voters,
      note: '按模板生成的测试数据。',
    })
  }
  return out
}

/*
 * 批量议题按项目轮询分配（见 buildBulkTopics），但有一个已知限制：
 * ensureTopics 靠**标题**判重，而标题里不带项目标识。所以如果某条议题
 * 在之前的运行里已经建到了 A 项目，这次轮询把它分给 B 项目时不会搬过去。
 *
 * 影响只限于「已经跑过一次的库」。需要重新分布时清掉批量议题再跑：
 *   DELETE FROM fb_topic WHERE title LIKE '%（议题 #%';
 * （以及它们的 fb_topic_answer / fb_answer_helpful）
 *
 * 因此每个项目「至少有一个议题」这件事不靠轮询保证 —— 像 dj66-creator
 * 这种一定要有的，直接写进上面的 TOPICS。
 */

/** 批量议题的素材。手写的那几个用来看页面长什么样，这些用来撑列表与分页。 */
const BULK_TOPIC_SEEDS = [
  { q: '你一般一次看几集？', hint: '说说你的习惯和原因', detail: '想知道大家是一集一集看，还是一口气刷完一季。这会影响我们怎么安排「下一集」的预加载。' },
  { q: '片头片尾你会跳过吗？', hint: '会 / 不会，以及为什么', detail: '如果多数人都跳，我们考虑把「记住跳过选择」做成默认。' },
  { q: '你更在意画质还是流量？', hint: '选一个，并说说你的网络环境', detail: '默认清晰度到底该给多少，想听听你们的实际网络情况。' },
  { q: '追剧提醒你希望怎么收到？', hint: '站内、推送、还是都不要', detail: '我们不想做成骚扰，所以先问清楚边界在哪。' },
  { q: '竖屏和横屏你更常用哪个？', hint: '说说使用场景', detail: '短剧本来是竖屏起家的，但横屏的人似乎在变多。' },
  { q: '封面图你更看重什么？', hint: '人物、文字、还是氛围', detail: '想优化一下封面的信息密度。' },
  { q: '搜索的时候你一般搜什么？', hint: '剧名、演员、还是剧情关键词', detail: '搜索联想该优先给哪一类，取决于这个。' },
  { q: '评论区你会看吗？', hint: '看 / 不看，以及什么时候看', detail: '决定评论区放在播放页的哪个位置。' },
  { q: '合集你会用吗？', hint: '说说你怎么整理想看的剧', detail: '在考虑把合集做成可以分享的。' },
  { q: '内测反馈的处理节奏你觉得合适吗？', hint: '太快 / 合适 / 太慢', detail: '我们现在是每周过一轮，想知道这个节奏体感如何。' },
  { q: '夜间模式你会主动开吗？', hint: '会 / 跟随系统 / 不用', detail: '决定深色模式的默认值。' },
  { q: '你愿意为无广告付费吗？', hint: '愿意 / 不愿意，以及心理价位', detail: '只是摸底，不代表马上要做。' },
]

const BULK_TOPIC_ANSWERS = [
  '我基本都是通勤路上看，一次一两集，所以最在意的是打开就能接着看。',
  '一口气刷完的那种，所以下一集的加载速度对我影响最大。',
  '看情况。周末会连着看，平时只看一集当下饭。',
  '我在意流量，公司网络还行但地铁上就很差了，希望能自动降档。',
  '画质优先。我大部分时间在 WiFi 下，宁愿等一下也要看清楚。',
  '站内提醒就够了，推送太多我会直接关掉。',
  '横屏多一些，我习惯横着拿手机看。',
  '封面上的文字别太多，一眼看不清反而不想点。',
  '评论区我会在看完一集之后翻一下，看别人怎么说。',
  '合集我用得挺多，主要是把想看的先存起来。',
  '节奏我觉得合适，至少能看到自己提的东西有回应。',
  '这个我没太强的偏好，跟系统走就行。',
]

/**
 * 边界内容。
 *
 * 这些不是为了「多」，是为了把排版压到极限：超长标题会不会把卡片撑破、
 * 没有换行的长串会不会横向溢出、只有一行的正文会不会让详情页塌掉。
 * 这类问题在正常长度的数据上永远测不出来。
 */
function buildEdgeCases(activeByProject) {
  const members = activeByProject.get('dj66-app') ?? []
  if (members.length === 0) return []
  const author = members[0]
  const longUrl =
    'https://www.example.com/very/long/path/that/never/breaks/because-it-has-no-spaces-at-all?from=seed&trace=0123456789abcdef0123456789abcdef'
  return [
    {
      project: 'dj66-app',
      author,
      kind: 'bug',
      title:
        '这是一个刻意写得非常长的反馈标题用来检查列表卡片和详情页标题在超出一行甚至两行之后会不会把布局撑破或者被截断得莫名其妙',
      body: `标题很长，正文很短。\n\n链接也故意不带空格：${longUrl}`,
      env: 'iPhone 15 Pro Max / iOS 19.2 / WiFi',
      status: 'triaged',
      votes: members.slice(0, 3),
      note: '边界用例：超长标题 + 不可断行的长链接。',
    },
    {
      project: 'dj66-app',
      author,
      kind: 'experience',
      title: '正文特别长的一条（排版压力测试）',
      body:
        Array.from(
          { length: 12 },
          (_, i) =>
            `第 ${i + 1} 段：这一段用来把详情页的正文区撑长，检查行高、段间距与阅读宽度在长文下是否仍然舒服。` +
            '中文段落没有空格，换行全靠排版引擎处理，所以必须用真实长度验证一次。',
        ).join('\n\n') + `\n\n末尾再放一个长串：${longUrl}`,
      env: 'Chrome 146 / macOS 26.1',
      status: 'planned',
      votes: members.slice(0, 5),
      note: '边界用例：超长正文。',
    },
    {
      project: 'dj66-app',
      author,
      kind: 'content',
      // 后端要求标题 4–160 字，所以这里取**恰好 4 个字**。
      // 边界用例要贴着边界，不要越过去——越过去测的是校验，不是排版。
      title: '想看武侠',
      body: '就一句话。',
      status: 'submitted',
      votes: [],
      note: '边界用例：标题取下限 4 字、正文仅一句。',
    },
    {
      project: 'dj66-app',
      author,
      kind: 'bug',
      title: '标题里有特殊字符 <script>alert(1)</script> & "引号" 还有 emoji 🎬🔥',
      body:
        '这条用来确认渲染时做了转义，不会把标签当 HTML 执行，也不会把 & 和引号弄坏。\n\n' +
        '正文里再来一次：<b>加粗</b> & <img src=x onerror=alert(1)>',
      env: 'Firefox 144 / Ubuntu 26.04',
      status: 'declined',
      votes: [],
      note: '边界用例：转义与 emoji。按社区规则这条不作为有效反馈处理。',
    },
  ]
}

/**
 * 批量议题，连带回答与「有道理」。
 *
 * 和批量反馈同一个道理：回答者与点「有道理」的人都必须是该项目的 active 成员，
 * 所以作者从 activeByProject 里挑。
 *
 * 刻意混入三种状态：征集中（多数）、已结束（用来看「已结束」标签页有没有内容）、
 * 以及零回答的议题（空状态在真实数据下长什么样）。
 */
function buildBulkTopics(count, activeByProject) {
  const rand = makeRandom(20261025)
  const pick = (arr) => arr[Math.floor(rand() * arr.length)]
  const slugs = ['dj66-app', 'dj66-web', 'dj66-tv', 'dj66-creator', 'fbe2e-demo'].filter(
    (s) => (activeByProject.get(s) ?? []).length >= 2,
  )
  if (slugs.length === 0) return []

  const out = []
  for (let i = 0; i < Math.min(count, BULK_TOPIC_SEEDS.length); i += 1) {
    const seed = BULK_TOPIC_SEEDS[i]
    /*
     * 按项目轮询分配，不要随机。
     *
     * 随机分配的结果是：某些项目一个 open 议题都没分到，项目详情页的「议题」
     * 就是空状态 —— 而那正是最需要有数据可看的地方。实际踩过：dj66-tv 随机
     * 只拿到一个 closed 议题，页面显示「这个项目还没有议题」。
     */
    const project = slugs[i % slugs.length]
    const members = activeByProject.get(project)

    // 每 5 个里留 1 个没有回答，用来看空状态
    const answerCount = i % 5 === 4 ? 0 : 2 + Math.floor(rand() * 4)
    const used = new Set()
    const answers = []
    for (let a = 0; a < answerCount; a += 1) {
      const author = members[Math.floor(rand() * members.length)]
      // 同一个人在一个议题里只回答一次，避免看起来像刷楼
      if (used.has(author)) continue
      used.add(author)
      const helpful = members.filter((h) => h !== author && rand() < 0.3)
      answers.push({
        author,
        body: `${pick(BULK_TOPIC_ANSWERS)}（样例回答 #${i + 1}-${a + 1}）`,
        helpful,
        // 每个议题最多一个精选，给第一条
        featured: a === 0 && rand() < 0.5,
      })
    }

    out.push({
      project,
      title: `${seed.q}（议题 #${String(i + 1).padStart(2, '0')}）`,
      body: seed.detail,
      answer_hint: seed.hint,
      /*
       * 已结束的议题只从「第二轮」开始出现（i >= slugs.length）。
       * 第一轮每个项目各发一个，必须都是 open —— 否则又会有项目只有 closed 议题，
       * 项目页照样是空的。
       */
      status: i >= slugs.length && i % 4 === 3 ? 'closed' : 'open',
      visibility: 'public',
      pinned: false,
      answers,
    })
  }
  return out
}

// ---------- 执行 ----------

const log = (...args) => console.log(...args)

async function loginAdmin() {
  const out = await call('POST', '/api/admin/login', {
    body: { username: ADMIN_USER, password: ADMIN_PASS },
    headers: { 'x-edge-secret': EDGE, origin: ADMIN_ORIGIN },
  })
  if (out.status !== 200) {
    throw new Error(
      `后台登录失败（${out.status}）。请确认账号 ${ADMIN_USER} 存在、已启用，` +
        `并通过 SEED_ADMIN_USER / SEED_ADMIN_PASS 指定正确的凭据。`,
    )
  }
  cookie = (out.headers.getSetCookie?.() ?? [])
    .map((c) => c.split(';')[0])
    .join('; ')
  csrf = out.body.data.csrf_token
  log(`后台已登录：${ADMIN_USER}`)
}

async function ensureProjects() {
  const existing = await admin('GET', '/api/admin/feedback/projects?page_size=100')
  const bySlug = new Map((existing.list ?? []).map((p) => [p.slug, p]))
  const out = new Map()
  for (const spec of PROJECTS) {
    const found = bySlug.get(spec.slug)
    if (found) {
      out.set(spec.slug, found)
      continue
    }
    const created = await admin('POST', '/api/admin/feedback/projects', {
      cover: '',
      try_url: '',
      ...spec,
    })
    out.set(spec.slug, created)
    log(`  + 项目 ${spec.name}`)
  }
  log(`项目就绪：${out.size} 个`)
  return out
}

/** 为某个项目新建一个长期可用的邀请码。明文只在创建响应里出现一次，所以每次都新建。 */
async function newInvite(projectId) {
  const res = await admin('POST', '/api/admin/feedback/invites', {
    project_id: projectId,
    role: 'member',
    max_uses: 50,
    note: '开发种子数据',
    expires_at: null,
  })
  return res.code
}

async function ensureUsers(invites) {
  const out = new Map()
  let added = 0
  for (const spec of USERS) {
    let res = await front('POST', '/api/v1/feedback/auth/login', {
      body: { handle: spec.handle, password: USER_PASS },
    })
    if (res.status !== 200) {
      res = await front('POST', '/api/v1/feedback/auth/register', {
        body: {
          // 字段顺序与注册表单一致：用户名、密码、昵称、邀请码。
          // 没有 ack_no_recovery —— 恢复码与「忘记密码」已删除（feedback/docs/adr/0011）。
          handle: spec.handle,
          password: USER_PASS,
          nickname: spec.nickname,
          invite_code: invites.get(spec.invite),
        },
      })
      if (res.status !== 200) {
        throw new Error(`注册 ${spec.handle} 失败：${res.status} ${res.raw.slice(0, 200)}`)
      }
      added += 1
      log(`  + 用户 ${spec.nickname}（内测 ${res.body.data.user.tester_no}）`)
    }
    out.set(spec.handle, res.body.data.token)
  }
  log(`用户就绪：${out.size} 个（新增 ${added}）`)
  return out
}

/**
 * 建立成员关系，并返回每个项目**真正能提交反馈的人**（active 成员）。
 *
 * 三种 join_mode 走三条路：
 *   open         → 直接 join，立刻是 active；
 *   application  → join 落成 pending，再由后台批准一部分成 active，
 *                  刻意留几个人停在 pending（前台「申请中」状态需要真实数据）；
 *   invite_only  → 不自助加入，靠注册时的项目邀请码带进去。
 *
 * 返回值很重要：bulk 反馈必须从该项目的 active 成员里挑作者。
 * 非成员提交会被后端拒掉，而那种失败在批量循环里很难定位。
 */
async function joinProjects(tokens, projects) {
  const creators = new Set(USERS.filter((u) => u.invite !== 'dj66-app').map((u) => u.handle))
  const testers = [...tokens].filter(([handle]) => !creators.has(handle))

  for (const spec of PROJECTS) {
    if (spec.join_mode === 'invite_only') continue
    for (const [, token] of testers) {
      await front('POST', `/api/v1/feedback/projects/${spec.slug}/join`, { token })
    }
  }

  // 需审核的项目：批准大部分，留最后 3 个人在 pending
  const PENDING_KEEP = 3
  for (const spec of PROJECTS) {
    if (spec.join_mode !== 'application') continue
    const projectID = projects.get(spec.slug).id
    const page = await admin(
      'GET',
      `/api/admin/feedback/projects/${projectID}/members?page_size=100`,
    )
    const pending = (page.list ?? []).filter((m) => m.status === 'pending')
    const approve = pending.slice(0, Math.max(0, pending.length - PENDING_KEEP))
    for (const m of approve) {
      await admin('PATCH', `/api/admin/feedback/projects/${projectID}/members/${m.user_id}`, {
        role: 'member',
        remove: false,
      })
    }
    log(
      `  ${spec.slug}：批准 ${approve.length} 人，留 ${pending.length - approve.length} 人在申请中`,
    )
  }

  // 回查每个项目的 active 成员（handle → 可提交反馈）
  const activeByProject = new Map()
  for (const spec of PROJECTS) {
    const projectID = projects.get(spec.slug).id
    const page = await admin(
      'GET',
      `/api/admin/feedback/projects/${projectID}/members?page_size=100`,
    )
    const handles = (page.list ?? [])
      .filter((m) => m.status === 'active' && tokens.has(m.handle))
      .map((m) => m.handle)
    activeByProject.set(spec.slug, handles)
  }
  log(
    '成员关系就绪：' +
      [...activeByProject].map(([slug, hs]) => `${slug} ${hs.length} 人`).join('、'),
  )
  return activeByProject
}

async function ensurePosts(projects) {
  const page = await admin('GET', '/api/admin/feedback/posts?page_size=100')
  const titles = new Set((page.list ?? []).map((p) => p.title))
  let added = 0
  for (const spec of POSTS) {
    if (titles.has(spec.title)) continue
    await admin('POST', '/api/admin/feedback/posts', {
      project_id: projects.get(spec.project).id,
      kind: spec.kind,
      title: spec.title,
      body: spec.body,
      status: spec.status,
      visibility: spec.visibility,
      pinned: spec.pinned,
    })
    added += 1
  }
  log(`动态就绪：新增 ${added} 条`)
}

/** 从 from 走到 to 的一条合法路径（与后端状态机一致）。 */
const PATH = {
  submitted: [],
  triaged: ['triaged'],
  planned: ['triaged', 'planned'],
  in_progress: ['triaged', 'in_progress'],
  verifying: ['triaged', 'in_progress', 'verifying'],
  shipped: ['triaged', 'in_progress', 'shipped'],
  closed: ['triaged', 'closed'],
  declined: ['triaged', 'declined'],
  duplicate: ['duplicate'],
}

/** 把后台反馈列表全部翻一遍，用来判断哪些已经造过。 */
async function allAdminFeedback() {
  const out = []
  for (let page = 1; page <= 20; page += 1) {
    const res = await admin('GET', `/api/admin/feedback/issues?page=${page}&page_size=100`)
    const list = res.list ?? []
    out.push(...list)
    if (out.length >= (res.total ?? 0) || list.length === 0) break
  }
  return out
}

async function ensureFeedback(tokens, specs) {
  const existing = await allAdminFeedback()
  const byTitle = new Map(existing.map((f) => [f.title, f]))
  const created = new Map()
  let added = 0

  for (const spec of specs) {
    let item = byTitle.get(spec.title)
    if (!item) {
      const token = tokens.get(spec.author)
      item = await frontOk('POST', '/api/v1/feedback/issues', {
        token,
        body: {
          project_slug: spec.project,
          kind: spec.kind,
          title: spec.title,
          body: spec.body,
          env: spec.env ?? '',
          page_url: '',
          visibility: spec.visibility ?? 'public',
        },
      })
      added += 1
    }
    created.set(spec.title, item)

    // 投票
    for (const voter of spec.votes ?? []) {
      await front('PUT', `/api/v1/feedback/issues/${spec.project}/${item.number}/vote`, {
        token: tokens.get(voter),
        body: { active: true },
      })
    }

    // 状态流转：已经到位就不动
    const detail = await admin('GET', `/api/admin/feedback/issues/${item.id}`)
    let current = detail.feedback
    if (current.status === spec.status) continue
    for (const step of PATH[spec.status] ?? []) {
      if (current.status === step) continue
      const body = {
        status: step,
        priority: current.priority,
        visibility: current.visibility,
        duplicate_of: 0,
        note: '',
        internal_note: '',
        version: current.version,
      }
      if (step === spec.status && spec.note) body.note = spec.note
      if (step === 'declined' || step === 'duplicate') body.note = spec.note ?? '按社区规则处理。'
      if (step === 'duplicate') {
        const main = created.get(spec.duplicateOfTitle)
        if (!main) throw new Error(`${spec.title} 找不到主反馈 ${spec.duplicateOfTitle}`)
        body.duplicate_of = main.id
      }
      if (step === 'triaged') {
        body.priority =
          spec.kind === 'bug' || spec.kind === 'access'
            ? (spec.votes?.length ?? 0) >= 4
              ? 'high'
              : 'medium'
            : 'low'
      }
      current = await admin('POST', `/api/admin/feedback/issues/${item.id}/triage`, body)
    }
  }
  log(`反馈就绪：新增 ${added} 条（共 ${specs.length} 条规格）`)
  return created
}

async function ensureTopics(projects, tokens, specs) {
  const page = await admin('GET', '/api/admin/feedback/topics?page_size=100')
  const byTitle = new Map((page.list ?? []).map((t) => [t.title, t]))
  let added = 0

  for (const spec of specs) {
    let topic = byTitle.get(spec.title)
    if (!topic) {
      // 先建成征集中，回答写完再按需要改状态——已结束的议题不收回答
      topic = await admin('POST', '/api/admin/feedback/topics', {
        project_id: projects.get(spec.project).id,
        title: spec.title,
        body: spec.body,
        answer_hint: spec.answer_hint,
        status: 'open',
        visibility: spec.visibility,
        pinned: spec.pinned,
        ends_at: null,
      })
      added += 1
    }

    const answers = await admin(
      'GET',
      `/api/admin/feedback/topics/${topic.id}/answers?page_size=100`,
    )
    const bodies = new Set((answers.list ?? []).map((a) => a.body))

    /*
     * 已结束的议题不收回答，强塞会被后端以 409 拒掉。
     *
     * 这种情况只在「库里已经是 closed」时出现：新建的议题一律先建成 open，
     * 回答写完才改状态。所以碰到 closed 就说明上一次运行已经把回答补齐了，
     * 跳过即可。（这正是改了状态分配规则之后重跑会撞上的坑。）
     */
    const alreadyClosed = topic.status === 'closed'

    for (const answer of spec.answers) {
      if (alreadyClosed) break
      if (bodies.has(answer.body)) continue
      const created = await frontOk('POST', `/api/v1/feedback/topics/${topic.id}/answers`, {
        token: tokens.get(answer.author),
        body: { parent_id: 0, body: answer.body },
      })
      for (const who of answer.helpful ?? []) {
        await front('PUT', `/api/v1/feedback/answers/${created.id}/helpful`, {
          token: tokens.get(who),
          body: { active: true },
        })
      }
      if (answer.featured) {
        await admin('POST', `/api/admin/feedback/answers/${created.id}/featured`, {
          featured: true,
        })
      }
    }

    /*
     * 状态双向同步，不是只「从 open 改成 closed」。
     *
     * 原来写的是 `if (spec.status !== 'open')`，意味着目标状态变回 open 时
     * 库里那条会一直停在 closed —— 改了状态分配规则之后重跑，某些议题就卡在
     * 已结束，项目详情页的「议题」区（只显示征集中）于是空着。
     */
    if (topic.status !== spec.status) {
      await admin('PATCH', `/api/admin/feedback/topics/${topic.id}`, {
        project_id: projects.get(spec.project).id,
        title: spec.title,
        body: spec.body,
        answer_hint: spec.answer_hint,
        status: spec.status,
        visibility: spec.visibility,
        pinned: spec.pinned,
        ends_at: null,
      })
    }
  }
  log(`议题就绪：新增 ${added} 个`)
}

async function main() {
  if (!EDGE || !SVC) throw new Error('.env 里缺 EDGE_SHARED_SECRET 或 BFF_SERVICE_TOKEN')
  const health = await call('GET', '/api/v1/health')
  if (health.status !== 200) throw new Error(`后端没起来（${health.status}），先跑 ./dev.sh up`)

  await loginAdmin()
  const projects = await ensureProjects()
  const invites = new Map([
    ['dj66-app', await newInvite(projects.get('dj66-app').id)],
    ['dj66-creator', await newInvite(projects.get('dj66-creator').id)],
  ])
  const tokens = await ensureUsers(invites)
  const activeByProject = await joinProjects(tokens, projects)
  await ensurePosts(projects)

  const bulkCount = Number(process.env.SEED_BULK ?? 220)
  // 手写的那批先造：批量数据里的「重复反馈」要指向它们
  await ensureFeedback(tokens, FEEDBACK)
  if (bulkCount > 0) {
    log(`批量生成 ${bulkCount} 条测试反馈…`)
    await ensureFeedback(tokens, buildBulk(bulkCount, activeByProject))
  }
  // 边界内容最后造：它们是刻意的极端值，不参与「重复反馈」的指向关系
  const edge = buildEdgeCases(activeByProject)
  if (edge.length > 0) {
    log(`生成 ${edge.length} 条边界内容（超长标题 / 超长正文 / 特殊字符）…`)
    await ensureFeedback(tokens, edge)
  }

  await ensureTopics(projects, tokens, TOPICS)

  const topicCount = Number(process.env.SEED_TOPICS ?? 10)
  if (topicCount > 0) {
    log(`批量生成 ${topicCount} 个议题（含回答与「有道理」）…`)
    await ensureTopics(projects, tokens, buildBulkTopics(topicCount, activeByProject))
  }

  log('\n完成。所有社区账号的密码都是：' + USER_PASS)
  log('刚造出来的数据时间都是「刚刚」，把它们摊到过去几周（让相对时间与排序看起来真实）：')
  log(
    '  docker compose -f compose.yaml --env-file .env exec -T postgres \\\n' +
      '    psql -U xsite_app -d xsite < feedback/scripts/spread-dev-time.sql',
  )
}

await main()
