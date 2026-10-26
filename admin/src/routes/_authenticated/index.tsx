import { createFileRoute } from '@tanstack/react-router'
import { pages } from '@/config/navigation'
import { PlaceholderFor } from '@/components/page-for'

export const Route = createFileRoute('/_authenticated/')({
  component: Dashboard,
})

function Dashboard() {
  return <PlaceholderFor page={pages[0]} />
}
