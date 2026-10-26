import type { Metadata } from 'next'
import { ProsePage } from '@/components/ui/prose'
import { TERMS } from '@/content/pages'
import { t } from '@/lib/i18n'

export const metadata: Metadata = { title: t('static.termsTitle') }

export default function TermsPage() {
  return <ProsePage title={t('static.termsTitle')} sections={TERMS} />
}
