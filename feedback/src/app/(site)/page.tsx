import { Suspense } from 'react'
import Link from 'next/link'
import { ArrowRight, Inbox, LayoutGrid, MessagesSquare, Rocket, Ticket } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { Container, SectionHeading } from '@/components/ui/section'
import { FeedbackCard } from '@/features/feedback/components/feedback-card'
import { PostCard } from '@/features/posts/components/post-card'
import { ProjectCard } from '@/features/projects/components/project-card'
import { TopicCard } from '@/features/topics/components/topic-card'
import {
  listPublicFeedback,
  listPublicPosts,
  listPublicProjects,
  listPublicTopics,
} from '@/lib/api/public-queries'
import { formatCount } from '@/lib/format'
import { t } from '@/lib/i18n'
import { cachedNow } from '@/lib/server/now'
import { getCurrentUser } from '@/lib/server/session'

/**
 * 首页（feedback/docs/06 第 3.2 节）。
 *
 * 目标是 30 秒内让人明白这是什么地方、现在的重点是什么、怎么参与。
 *
 * ── 这一页是静态页（feedback/docs/06 第 11.5 节）──
 *
 * 所有列表都走 public-queries 的公开视角，不带访问者身份，因此可以被缓存
 * 并在构建时预渲染。只有两处需要知道「你是谁」：Hero 的次要按钮和底部 CTA，
 * 它们各自收在 Suspense 里，运行时流式填进来。
 *
 * 换来的变化：首页列表只展示公开内容。「仅成员可见」的项目与反馈不在这里出现，
 * 要看它们请去 /projects、/me —— 那两页仍按访问者身份实时渲染。
 *
 * 数据逐个 settle：某一块后端不可用时只有那一块显示空状态，
 * 不会让整个首页变成错误页——首页是入口，哪怕只剩一半内容也要能用。
 */
export default async function HomePage() {
  const now = await cachedNow()
  const [projects, posts, feedback, topics, shipped] = await Promise.all([
    settle(() => listPublicProjects(6)),
    settle(() => listPublicPosts(3)),
    settle(() => listPublicFeedback({ page_size: 6, sort: 'latest' })),
    settle(() => listPublicTopics(2)),
    settle(() => listPublicFeedback({ page_size: 1, status: 'shipped' })),
  ])

  return (
    <>
      <Hero
        projectCount={projects?.total ?? 0}
        feedbackCount={feedback?.total ?? 0}
        shippedCount={shipped?.total ?? 0}
      />

      <Container className="flex flex-col gap-14 py-12 sm:gap-16 sm:py-14">
        {/* 当前重点：团队最近发出的测试任务与上线小结 */}
        <section aria-labelledby="focus-heading">
          <SectionHeading
            title={t('home.focusTitle')}
            hint={t('home.focusHint')}
            moreHref="/updates"
          />
          <div id="focus-heading" className="sr-only">
            {t('home.focusTitle')}
          </div>
          {posts && posts.list.length > 0 ? (
            // auto-rows-fr：同一行的卡片等高，摘要长短不一时底部对齐
            <div className="grid auto-rows-fr gap-4 md:grid-cols-2 lg:grid-cols-3">
              {posts.list.map((post) => (
                <PostCard key={post.id} post={post} now={now} />
              ))}
            </div>
          ) : (
            <EmptyState icon={Rocket} title={t('home.emptyFocus')} hint={t('state.emptyHint')} />
          )}
        </section>

        {/* 活跃项目 */}
        <section aria-labelledby="projects-heading">
          <SectionHeading
            title={t('home.projectsTitle')}
            hint={t('home.projectsHint')}
            moreHref="/projects"
          />
          <div id="projects-heading" className="sr-only">
            {t('home.projectsTitle')}
          </div>
          {projects && projects.list.length > 0 ? (
            <div className="grid auto-rows-fr gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {projects.list.map((project) => (
                <ProjectCard key={project.id} project={project} />
              ))}
            </div>
          ) : (
            <EmptyState
              icon={LayoutGrid}
              title={t('home.emptyProjects')}
              hint={t('state.emptyHint')}
            />
          )}
        </section>

        {/* 反馈 + 议题：桌面并排，移动端堆叠（反馈在前，它是这个站的主线） */}
        <div className="grid gap-12 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)] lg:gap-10">
          <section aria-labelledby="feedback-heading">
            <SectionHeading
              title={t('home.feedbackTitle')}
              hint={t('home.feedbackHint')}
              moreHref="/feedback"
            />
            <div id="feedback-heading" className="sr-only">
              {t('home.feedbackTitle')}
            </div>
            {feedback && feedback.list.length > 0 ? (
              <div className="flex flex-col gap-3">
                {feedback.list.map((item) => (
                  <FeedbackCard key={item.id} item={item} now={now} />
                ))}
              </div>
            ) : (
              <EmptyState
                icon={Inbox}
                title={t('home.emptyFeedback')}
                hint={t('state.emptyHint')}
                action={
                  <Button asChild size="sm">
                    <Link href="/feedback/new">{t('action.newFeedback')}</Link>
                  </Button>
                }
              />
            )}
          </section>

          <section aria-labelledby="topics-heading">
            <SectionHeading
              title={t('home.topicTitle')}
              hint={t('home.topicHint')}
              moreHref="/topics"
            />
            <div id="topics-heading" className="sr-only">
              {t('home.topicTitle')}
            </div>
            {topics && topics.list.length > 0 ? (
              <div className="flex flex-col gap-4">
                {topics.list.map((topic) => (
                  <TopicCard key={topic.id} topic={topic} now={now} />
                ))}
              </div>
            ) : (
              <EmptyState
                icon={MessagesSquare}
                title="最近没有在征集的议题"
                hint="项目团队发起议题后会出现在这里。"
              />
            )}
          </section>
        </div>

        <HowItWorks />
      </Container>

      {/*
        底部 CTA 只给未登录访客看，所以必须等会话。fallback 给 null：
        这是整页最后一块，晚一点出现不会推动上面任何内容（不产生 CLS）。
      */}
      <Suspense fallback={null}>
        <BottomCtaSlot />
      </Suspense>
    </>
  )
}

/** 读会话决定是否展示注册引导。登录用户不需要再被劝一次注册。 */
async function BottomCtaSlot() {
  const user = await getCurrentUser().catch(() => null)
  if (user) return null
  return <BottomCta />
}

/**
 * Hero 的次要按钮：登录了去「提交反馈」，没登录去「了解如何参与」。
 *
 * 单独拆出来是为了让 Hero 其余部分留在静态 HTML 里 ——
 * 标题、正文、三个统计数字都不依赖身份，没必要跟着这一个按钮一起变成动态。
 */
async function HeroSecondaryAction() {
  const user = await getCurrentUser().catch(() => null)
  return (
    <Button asChild size="lg" variant="outline" className="sm:min-w-36">
      <Link href={user ? '/feedback/new' : '/guide'}>
        {user ? t('action.newFeedback') : t('action.howItWorks')}
      </Link>
    </Button>
  )
}

/** 单块数据失败不拖垮整页。只吞读失败，写操作绝不这样处理。 */
async function settle<T>(run: () => Promise<T>): Promise<T | null> {
  try {
    return await run()
  } catch {
    return null
  }
}

function Hero({
  projectCount,
  feedbackCount,
  shippedCount,
}: {
  projectCount: number
  feedbackCount: number
  shippedCount: number
}) {
  const stats = [
    { label: t('home.statProjects'), value: projectCount },
    { label: t('home.statFeedback'), value: feedbackCount },
    { label: t('home.statShipped'), value: shippedCount },
  ]
  return (
    <section className="border-b border-border bg-surface">
      <Container className="grid gap-10 py-12 sm:py-16 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end lg:gap-16 lg:py-20">
        <div className="max-w-prose">
          <p className="text-xs font-semibold tracking-[0.12em] text-primary-fg uppercase">
            {t('home.heroEyebrow')}
          </p>
          <h1 className="mt-3 text-3xl font-semibold tracking-tight text-fg sm:text-4xl lg:text-5xl">
            {t('home.heroTitle')}
          </h1>
          <p className="mt-4 text-base text-fg-muted sm:text-lg">{t('home.heroBody')}</p>
          <div className="mt-7 flex flex-col gap-3 sm:flex-row">
            <Button asChild size="lg">
              <Link href="/projects">
                {t('action.browseProjects')}
                <ArrowRight aria-hidden />
              </Link>
            </Button>
            {/*
              占位用「了解如何参与」而不是空白：绝大多数首次访问者是未登录的，
              所以这个 fallback 多半就是最终结果，不会出现替换时的文字跳变。
              min-w 让两种文案宽度一致，避免替换瞬间按钮变宽。
            */}
            <Suspense
              fallback={
                <Button asChild size="lg" variant="outline" className="sm:min-w-36">
                  <Link href="/guide">{t('action.howItWorks')}</Link>
                </Button>
              }
            >
              <HeroSecondaryAction />
            </Suspense>
          </div>
        </div>

        {/*
          这三个数字全部来自接口的 total，而且是「当前访问者可见范围内」的真实计数。
          文档明确禁止伪造社交证明，所以这里不做兜底的假数据——真为 0 就显示 0。
        */}
        <dl className="grid grid-cols-3 gap-6 border-t border-border pt-6 lg:border-t-0 lg:border-s lg:border-border lg:ps-10 lg:pt-0">
          {stats.map((stat) => (
            <div key={stat.label} className="min-w-0">
              <dd className="text-2xl font-semibold text-fg tabular sm:text-3xl">
                {formatCount(stat.value)}
              </dd>
              <dt className="mt-1 text-xs text-fg-muted">{stat.label}</dt>
            </div>
          ))}
        </dl>
      </Container>
    </section>
  )
}

function HowItWorks() {
  const steps = [
    { icon: Ticket, title: t('home.howStep1Title'), body: t('home.howStep1Body') },
    { icon: LayoutGrid, title: t('home.howStep2Title'), body: t('home.howStep2Body') },
    { icon: Inbox, title: t('home.howStep3Title'), body: t('home.howStep3Body') },
  ]
  return (
    <section aria-labelledby="how-heading">
      <h2 id="how-heading" className="text-lg font-semibold tracking-tight text-fg sm:text-xl">
        {t('home.howTitle')}
      </h2>
      <ol className="mt-5 grid gap-6 md:grid-cols-3">
        {steps.map((step, index) => {
          const Icon = step.icon
          return (
            <li key={step.title} className="flex gap-4">
              <span
                className="flex size-10 shrink-0 items-center justify-center rounded-pill bg-primary-surface text-primary-fg"
                aria-hidden
              >
                <Icon className="size-5" />
              </span>
              <div className="min-w-0">
                <h3 className="text-sm font-semibold text-fg">
                  <span className="text-fg-subtle tabular">{index + 1}. </span>
                  {step.title}
                </h3>
                <p className="mt-1.5 text-sm text-fg-muted">{step.body}</p>
              </div>
            </li>
          )
        })}
      </ol>
    </section>
  )
}

function BottomCta() {
  return (
    <section className="border-t border-border bg-primary-surface">
      <Container className="flex flex-col items-start gap-5 py-12 sm:flex-row sm:items-center sm:justify-between sm:py-14">
        <div className="max-w-prose">
          <h2 className="text-xl font-semibold tracking-tight text-fg">{t('home.ctaTitle')}</h2>
          <p className="mt-2 text-sm text-fg-muted">{t('home.ctaBody')}</p>
        </div>
        <div className="flex shrink-0 gap-3">
          <Button asChild size="lg">
            <Link href="/register">{t('nav.register')}</Link>
          </Button>
          <Button asChild size="lg" variant="outline">
            <Link href="/login">{t('nav.login')}</Link>
          </Button>
        </div>
      </Container>
    </section>
  )
}
