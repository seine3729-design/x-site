import { createFileRoute, Link, useLocation } from '@tanstack/react-router'
import { findPageByPath } from '@/config/navigation'
import { PlaceholderFor } from '@/components/page-for'
import { Button } from '@/components/ui/button'

// 尚未实现的内置页面统一由这里渲染占位页；模块实现时新增具体路由文件即可覆盖。
export const Route = createFileRoute('/_authenticated/$')({
  component: PlaceholderOrNotFound,
})

function PlaceholderOrNotFound() {
  const { pathname } = useLocation()
  const page = findPageByPath(pathname)

  if (page) {
    return <PlaceholderFor page={page} />
  }

  return (
    <div className='flex flex-col items-start gap-3'>
      <h1 className='text-2xl font-semibold'>页面不存在</h1>
      <p className='text-muted-foreground'>地址 {pathname} 不是后台页面。</p>
      <Button asChild variant='outline'>
        <Link to='/'>回到看板</Link>
      </Button>
    </div>
  )
}
