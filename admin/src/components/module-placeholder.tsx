import { Construction, type LucideIcon } from 'lucide-react'
import type { PageInfo } from '@/config/navigation'
import { editOf, viewOf } from '@/lib/permissions'
import { useCan } from '@/stores/auth-store'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

type ModulePlaceholderProps = {
  page: PageInfo
  /** 后台菜单中配置的名称与图标（菜单被隐藏时用页面登记表中的默认名称）。 */
  title: string
  icon: LucideIcon
  groupTitle?: string
}

/** 尚未实现的模块页：说明设计文档位置与任务编号。模块实现后用具体路由文件替换。 */
export function ModulePlaceholder({ page, title, icon: Icon, groupTitle }: ModulePlaceholderProps) {
  const can = useCan()
  if (!can(viewOf(page.code))) {
    return <NoPermission title={title} />
  }
  const editable = can(editOf(page.code))

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex items-center gap-3'>
        <Icon className='size-6 text-muted-foreground' aria-hidden />
        <h1 className='text-2xl font-semibold tracking-tight'>{title}</h1>
        {groupTitle && <Badge variant='outline'>{groupTitle}</Badge>}
        {!editable && <Badge variant='secondary'>只读</Badge>}
      </div>
      <Card>
        <CardHeader>
          <CardTitle className='flex items-center gap-2 text-base'>
            <Construction className='size-4' aria-hidden />
            模块建设中
          </CardTitle>
          <CardDescription>
            功能设计见 {page.doc}，开发任务 {page.task}（docs/11）。
          </CardDescription>
        </CardHeader>
        <CardContent className='text-sm text-muted-foreground'>
          {editable
            ? '当前角色可修改本模块数据。'
            : '当前角色对本模块只有查看权限，表单与操作按钮会被禁用。'}
        </CardContent>
      </Card>
    </div>
  )
}

/** 没有页面「查看」权限时的提示（后端接口同样会拒绝）。 */
export function NoPermission({ title }: { title: string }) {
  return (
    <div className='flex flex-col gap-2'>
      <h1 className='text-2xl font-semibold tracking-tight'>{title}</h1>
      <p className='text-muted-foreground'>当前角色没有查看本页面的权限，如需访问请联系超级管理员在「权限矩阵」中授权。</p>
    </div>
  )
}
