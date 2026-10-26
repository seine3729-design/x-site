import 'server-only'
import { cacheLife, cacheTag } from 'next/cache'
import { callPublicService } from '@/lib/server/api'
import { pageOf } from './page'
import type { FbFeedback, FbPost, FbProjectView, FbTopic, Page } from './types'

/**
 * 公开视角的读接口（feedback/docs/06 第 11.5 节）。
 *
 * 和 queries.ts 的区别只有一个，但很关键：**这里不带访问者身份**。
 * 后端按匿名过滤，返回的就是「任何人都能看」的那一部分，所以结果可以进
 * 共享缓存、可以在构建时预渲染，不存在串号的可能。
 *
 * 这是首页能做成静态页的前提。代价是首页的列表只展示公开内容 ——
 * 对一个对外的橱窗页来说这是正确的；要看「仅成员可见」的项目请去 /projects、/me，
 * 那两个页面仍然按访问者身份实时渲染。
 *
 * ⚠️ 不要把带个人数据的接口搬进这个文件。判断标准很简单：
 *    同一个地址对不同人返回不同内容的，就不能放这里。
 */

/**
 * 缓存档位。
 *
 * 用 minutes 而不是更长：首页要显示「3 分钟前」这类相对时间，
 * 基准时间是实时的（requestNow），但数据里的时间戳来自缓存，
 * 档位拉到小时级就会出现「刚刚」其实是一小时前的情况。
 *
 * cacheTag 让运营后台改完内容后可以按标签主动失效，不用等自然过期。
 */
function homeCache(tag: string) {
  cacheLife('minutes')
  cacheTag('fb-public', tag)
}

/**
 * 取不到就返回空页，不要让错误冒出缓存边界。
 *
 * 首页是静态页，它的数据在**构建时**就要取一次。如果让错误抛出去，
 * 后端不可达的那次构建会直接失败 —— CI 里后端通常根本没起，
 * 不能因为这个就发不出版本。
 *
 * 所以这里吞掉错误、渲染空状态，运行时缓存过期后自然会补上真实数据。
 * 但一定要留日志：首页空了的时候，这行是唯一的线索。
 */
async function orEmpty<T>(label: string, run: () => Promise<Page<T>>): Promise<Page<T>> {
  try {
    return await run()
  } catch (error) {
    console.error(`[feedback] 首页公开数据取不到（${label}），本次渲染为空状态：`, error)
    return pageOf<T>(null)
  }
}

/** 活跃项目（公开可见的）。 */
export async function listPublicProjects(pageSize = 6): Promise<Page<FbProjectView>> {
  'use cache'
  homeCache('fb-projects')
  return orEmpty('projects', async () =>
    pageOf(
      await callPublicService<Page<FbProjectView>>('/api/v1/feedback/projects', {
        query: { page_size: pageSize },
      }),
    ),
  )
}

/** 项目团队发的动态。 */
export async function listPublicPosts(pageSize = 3): Promise<Page<FbPost>> {
  'use cache'
  homeCache('fb-posts')
  return orEmpty('posts', async () =>
    pageOf(
      await callPublicService<Page<FbPost>>('/api/v1/feedback/posts', {
        query: { page_size: pageSize },
      }),
    ),
  )
}

/** 公开反馈。status 用于首页的「已随版本上线」计数。 */
export async function listPublicFeedback(
  params: { page_size?: number; sort?: string; status?: string } = {},
): Promise<Page<FbFeedback>> {
  'use cache'
  homeCache('fb-issues')
  return orEmpty('issues', async () =>
    pageOf(
      await callPublicService<Page<FbFeedback>>('/api/v1/feedback/issues', { query: params }),
    ),
  )
}

/** 正在征集的议题。 */
export async function listPublicTopics(pageSize = 2): Promise<Page<FbTopic>> {
  'use cache'
  homeCache('fb-topics')
  return orEmpty('topics', async () =>
    pageOf(
      await callPublicService<Page<FbTopic>>('/api/v1/feedback/topics', {
        query: { status: 'open', page_size: pageSize },
      }),
    ),
  )
}
