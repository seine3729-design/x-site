import {
  LogOut,
  ShieldCheck,
  Sparkles,
  ChevronsUpDown,
} from 'lucide-react'
import useDialogState from '@/hooks/use-dialog-state'
import { AccountSecurityDialog } from '@/features/auth/account-security-dialog'
import { Avatar, AvatarImage } from '@/components/ui/avatar'
import { useLayoutStorage } from '@/hooks/use-layout-storage'
import { type ConfigDrawerRef } from '@/components/config-drawer'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuGroup,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from '@/components/ui/sidebar'
import { SignOutDialog } from '@/components/sign-out-dialog'

type NavUserProps = {
  user: {
    name: string
    avatar: string
  }
  configDrawerRef?: React.RefObject<ConfigDrawerRef | null>
}

export function NavUser({ user, configDrawerRef }: NavUserProps) {
  const [open, setOpen] = useDialogState()
  const [securityOpen, setSecurityOpen] = useDialogState()
  const { settings } = useLayoutStorage()
  
  const avatarMap: Record<string, { src: string; label: string }> = {
    mouse: { src: '/src/assets/image/mouse.webp', label: '鼠' },
    ox: { src: '/src/assets/image/ox.webp', label: '牛' },
    tiger: { src: '/src/assets/image/tiger.webp', label: '虎' },
    rabbit: { src: '/src/assets/image/rabbit.webp', label: '兔' },
    dragon: { src: '/src/assets/image/dragon.webp', label: '龙' },
    snake: { src: '/src/assets/image/snake.webp', label: '蛇' },
    horse: { src: '/src/assets/image/horse.webp', label: '马' },
    sheep: { src: '/src/assets/image/sheep.webp', label: '羊' },
    monkey: { src: '/src/assets/image/monkey.webp', label: '猴' },
    chicken: { src: '/src/assets/image/chicken.webp', label: '鸡' },
    dog: { src: '/src/assets/image/dog.webp', label: '狗' },
    pig: { src: '/src/assets/image/pig.webp', label: '猪' },
  }
  
  const currentAvatar = avatarMap[settings.avatar]
  const avatarSrc = currentAvatar?.src || user.avatar
  const avatarLabel = currentAvatar?.label || user.name

  return (
    <>
      <SidebarMenu>
        <SidebarMenuItem>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <SidebarMenuButton
                size='lg'
                className='data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground'
              >
                <Avatar className='h-8 w-8 rounded-full ring-2 ring-border'>
                  <AvatarImage src={avatarSrc} alt={avatarLabel} title={avatarLabel} className='rounded-full' />
                </Avatar>
                <div className='grid flex-1 text-start text-sm leading-tight'>
                  <span className='truncate font-semibold'>{user.name}</span>
                </div>
                <ChevronsUpDown className='ms-auto size-4' />
              </SidebarMenuButton>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              className='w-(--radix-dropdown-menu-trigger-width) min-w-32'
              side='bottom'
              align='end'
              sideOffset={4}
            >
              <DropdownMenuGroup>
                <DropdownMenuItem
                  className='cursor-pointer'
                  onClick={() => setSecurityOpen(true)}
                >
                  <ShieldCheck />
                  账号安全
                </DropdownMenuItem>
                <DropdownMenuItem 
                  className='cursor-pointer'
                  onClick={() => configDrawerRef?.current?.open()}
                >
                  <Sparkles />
                  系统设置
                </DropdownMenuItem>
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                className='cursor-pointer'
                variant='destructive'
                onClick={() => setOpen(true)}
              >
                <LogOut />
                退出登录
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </SidebarMenuItem>
      </SidebarMenu>

      <SignOutDialog open={!!open} onOpenChange={setOpen} />
      <AccountSecurityDialog open={!!securityOpen} onOpenChange={setSecurityOpen} />
    </>
  )
}
