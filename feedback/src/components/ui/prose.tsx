import { Container } from '@/components/ui/section'
import { t } from '@/lib/i18n'
import type { Section } from '@/content/pages'

/**
 * 长文页（参与指南、隐私说明、社区规则）。
 *
 * 行长控制在读栏宽度内：中文主栏 32–42 字一行最舒服（docs/06 第 2.4 节），
 * 整页铺满 1200px 会让人读到一半找不到下一行在哪。
 */
export function ProsePage({ title, sections }: { title: string; sections: Section[] }) {
  return (
    <Container className="py-8 sm:py-12">
      <article className="mx-auto max-w-prose">
        <h1 className="text-2xl font-semibold tracking-tight text-fg sm:text-3xl">{title}</h1>
        <p className="mt-2 text-xs text-fg-subtle">{t('static.updatedAt')}</p>

        <div className="mt-8 flex flex-col gap-9">
          {sections.map((section) => (
            <section key={section.title}>
              <h2 className="text-lg font-semibold tracking-tight text-fg">{section.title}</h2>
              {section.paragraphs.map((paragraph) => (
                <p key={paragraph} className="mt-3 text-sm leading-relaxed text-fg-muted">
                  {paragraph}
                </p>
              ))}
              {section.list && (
                <ul className="mt-3 flex flex-col gap-2">
                  {section.list.map((item) => (
                    <li key={item} className="flex gap-2.5 text-sm leading-relaxed text-fg-muted">
                      <span className="mt-2 size-1.5 shrink-0 rounded-pill bg-border-strong" aria-hidden />
                      <span className="min-w-0">{item}</span>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          ))}
        </div>
      </article>
    </Container>
  )
}
