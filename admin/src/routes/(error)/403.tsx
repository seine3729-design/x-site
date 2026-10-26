import { createFileRoute, useNavigate, useRouter } from '@tanstack/react-router'
import { ShieldAlert } from 'lucide-react'
import { Button } from '@/components/ui/button'

export const Route = createFileRoute('/(error)/403')({
  component: Forbidden,
})

function Forbidden() {
  const navigate = useNavigate()
  const router = useRouter()

  const handleGoBack = () => {
    if (router.history.length > 1) {
      window.history.back()
    } else {
      navigate({ to: '/' })
    }
  }

  return (
    <div className="flex min-h-screen flex-col items-center justify-center bg-background">
      <div className="flex flex-col items-center space-y-6 text-center">
        <ShieldAlert className="h-24 w-24 text-destructive" />
        <div className="space-y-2">
          <h1 className="text-4xl font-bold tracking-tight">403</h1>
          <h2 className="text-2xl font-semibold">无权限访问</h2>
          <p className="text-muted-foreground max-w-md">
            抱歉，您没有权限访问此页面或资源。如需访问，请联系管理员。
          </p>
        </div>
        <div className="flex gap-4">
          <Button
            variant="outline"
            onClick={handleGoBack}
          >
            返回上一页
          </Button>
          <Button onClick={() => navigate({ to: '/' })}>
            返回首页
          </Button>
        </div>
      </div>
    </div>
  )
}
