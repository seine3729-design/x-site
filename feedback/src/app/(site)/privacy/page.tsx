import type { Metadata } from 'next'
import { ProsePage } from '@/components/ui/prose'
import { PRIVACY } from '@/content/pages'
import { t } from '@/lib/i18n'

export const metadata: Metadata = { title: t('static.privacyTitle') }

export default function PrivacyPage() {
  return <ProsePage title={t('static.privacyTitle')} sections={PRIVACY} />
}
