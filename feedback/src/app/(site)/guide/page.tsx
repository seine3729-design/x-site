import type { Metadata } from 'next'
import { ProsePage } from '@/components/ui/prose'
import { GUIDE } from '@/content/pages'
import { t } from '@/lib/i18n'

export const metadata: Metadata = {
  title: t('static.guideTitle'),
  description: GUIDE[0]?.paragraphs[0],
}

export default function GuidePage() {
  return <ProsePage title={t('static.guideTitle')} sections={GUIDE} />
}
