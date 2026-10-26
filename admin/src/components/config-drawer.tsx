import { type SVGProps, forwardRef, useImperativeHandle, useState } from 'react'
import { RadioGroup } from 'radix-ui'
import { CircleCheck, RotateCcw, User } from 'lucide-react'
import { IconLayoutCompact } from '@/assets/custom/icon-layout-compact'
import { IconLayoutDefault } from '@/assets/custom/icon-layout-default'
import { IconLayoutFull } from '@/assets/custom/icon-layout-full'
import { IconSidebarFloating } from '@/assets/custom/icon-sidebar-floating'
import { IconSidebarInset } from '@/assets/custom/icon-sidebar-inset'
import { IconSidebarSidebar } from '@/assets/custom/icon-sidebar-sidebar'
import { IconThemeDark } from '@/assets/custom/icon-theme-dark'
import { IconThemeLight } from '@/assets/custom/icon-theme-light'
import { IconThemeSystem } from '@/assets/custom/icon-theme-system'
import { cn } from '@/lib/utils'
import { type Collapsible, useLayout } from '@/context/layout-provider'
import { useTheme } from '@/context/theme-provider'
import { useLayoutStorage } from '@/hooks/use-layout-storage'
import { Button, Avatar, AvatarFallback, AvatarImage } from '@/components/ui'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { useSidebar } from './ui/sidebar'

export interface ConfigDrawerRef {
  open: () => void
  close: () => void
}

export const ConfigDrawer = forwardRef<ConfigDrawerRef>((_, ref) => {
  const [isOpen, setIsOpen] = useState(false)

  useImperativeHandle(ref, () => ({
    open: () => setIsOpen(true),
    close: () => setIsOpen(false),
  }))

  return (
    <Sheet open={isOpen} onOpenChange={setIsOpen}>
      <SheetContent className='flex flex-col' aria-describedby={undefined}>
        <SheetHeader className='pb-0 text-start'>
          <SheetTitle>主题设置</SheetTitle>
          <SheetDescription id='config-drawer-description'>
            调整外观和布局以适应您的偏好
          </SheetDescription>
        </SheetHeader>
        <div className='space-y-6 overflow-y-auto px-4'>
          <AvatarConfig />
          <ThemeConfig />
          <SidebarConfig />
          <LayoutConfig />
        </div>
      </SheetContent>
    </Sheet>
  )
})

ConfigDrawer.displayName = 'ConfigDrawer'

function SectionTitle({
  title,
  showReset = false,
  onReset,
  className,
}: {
  title: string
  showReset?: boolean
  onReset?: () => void
  className?: string
}) {
  return (
    <div
      className={cn(
        'mb-2 flex items-center gap-2 text-sm text-muted-foreground',
        className
      )}
    >
      {title}
      {showReset && onReset && (
        <Button
          size='icon'
          variant='secondary'
          className='size-4 rounded-full'
          onClick={onReset}
        >
          <RotateCcw className='size-3' />
        </Button>
      )}
    </div>
  )
}

function RadioGroupItem({
  item,
  isTheme = false,
}: {
  item: {
    value: string
    label: string
    icon: (props: SVGProps<SVGSVGElement>) => React.ReactElement
  }
  isTheme?: boolean
}) {
  return (
    <RadioGroup.Item
      value={item.value}
      className={cn('group outline-none', 'transition duration-200 ease-in')}
    >
      <div
        className={cn(
          'relative rounded-[6px] ring-[1px] ring-border',
          'group-data-[state=checked]:shadow-2xl group-data-[state=checked]:ring-primary',
          'group-focus-visible:ring-2'
        )}
        role='img'
        aria-hidden='false'
        aria-label={`${item.label} option preview`}
      >
        <CircleCheck
          className={cn(
            'size-6 fill-primary stroke-white',
            'group-data-[state=unchecked]:hidden',
            'absolute top-0 right-0 translate-x-1/2 -translate-y-1/2'
          )}
          aria-hidden='true'
        />
        <item.icon
          className={cn(
            !isTheme &&
              'fill-primary stroke-primary group-data-[state=unchecked]:fill-muted-foreground group-data-[state=unchecked]:stroke-muted-foreground'
          )}
          aria-hidden='true'
        />
      </div>
      <div
        className='mt-1 text-xs'
        id={`${item.value}-description`}
        aria-live='polite'
      >
        {item.label}
      </div>
    </RadioGroup.Item>
  )
}

// 头像配置
function AvatarConfig() {
  const { settings, updateSettings } = useLayoutStorage()
  const avatars = [
    { name: 'mouse', avatar: '/src/assets/image/mouse.webp', label: '鼠' },
    { name: 'ox', avatar: '/src/assets/image/ox.webp', label: '牛' },
    { name: 'tiger', avatar: '/src/assets/image/tiger.webp', label: '虎' },
    { name: 'rabbit', avatar: '/src/assets/image/rabbit.webp', label: '兔' },
    { name: 'dragon', avatar: '/src/assets/image/dragon.webp', label: '龙' },
    { name: 'snake', avatar: '/src/assets/image/snake.webp', label: '蛇' },
    { name: 'horse', avatar: '/src/assets/image/horse.webp', label: '马' },
    { name: 'sheep', avatar: '/src/assets/image/sheep.webp', label: '羊' },
    { name: 'monkey', avatar: '/src/assets/image/monkey.webp', label: '猴' },
    { name: 'chicken', avatar: '/src/assets/image/chicken.webp', label: '鸡' },
    { name: 'dog', avatar: '/src/assets/image/dog.webp', label: '狗' },
    { name: 'pig', avatar: '/src/assets/image/pig.webp', label: '猪' },
  ]

  return (
    <div>
      <SectionTitle title='头像选择' />
      <div className='grid grid-cols-6 gap-3' aria-describedby='avatar-description'>
        {avatars.map((item) => (
          <div key={item.name} className='flex flex-col items-center gap-1'>
            <button
              onClick={() => updateSettings({ avatar: item.name })}
              className={cn(
                'relative rounded-full ring-2 transition-all',
                settings.avatar === item.name
                  ? 'scale-110 ring-primary'
                  : 'ring-transparent hover:ring-border'
              )}
              title={item.label}
            >
              <Avatar className='h-12 w-12'>
                <AvatarImage src={item.avatar} alt={item.label} />
                <AvatarFallback>
                  <User className='h-6 w-6' />
                </AvatarFallback>
              </Avatar>
              {settings.avatar === item.name && (
                <CircleCheck
                  className='absolute -top-1 -right-1 h-5 w-5 fill-primary stroke-white'
                  aria-hidden='true'
                />
              )}
            </button>
            <span className='text-xs text-muted-foreground'>{item.label}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

// 主题配置
function ThemeConfig() {
  const { defaultTheme, theme, setTheme } = useTheme()
  return (
    <div>
      <SectionTitle
        title='主题模式'
        showReset={theme !== defaultTheme}
        onReset={() => setTheme(defaultTheme)}
      />
      <RadioGroup.Root
        value={theme}
        onValueChange={setTheme}
        className='grid w-full max-w-md grid-cols-3 gap-4'
        aria-label='选择主题偏好'
      >
        {[
          {
            value: 'system',
            label: '跟随系统',
            icon: IconThemeSystem,
          },
          {
            value: 'light',
            label: '浅色',
            icon: IconThemeLight,
          },
          {
            value: 'dark',
            label: '深色',
            icon: IconThemeDark,
          },
        ].map((item) => (
          <RadioGroupItem key={item.value} item={item} isTheme />
        ))}
      </RadioGroup.Root>
      <div id='theme-description' className='sr-only'>
        在系统偏好、浅色模式或深色模式之间选择
      </div>
    </div>
  )
}

// 侧边栏配置
function SidebarConfig() {
  const { defaultVariant, variant, setVariant } = useLayout()
  return (
    <div className='max-md:hidden'>
      <SectionTitle
        title='侧边栏'
        showReset={defaultVariant !== variant}
        onReset={() => setVariant(defaultVariant)}
      />
      <RadioGroup.Root
        value={variant}
        onValueChange={setVariant}
        className='grid w-full max-w-md grid-cols-3 gap-4'
        aria-label='选择侧边栏样式'
      >
        {[
          {
            value: 'inset',
            label: '内嵌',
            icon: IconSidebarInset,
          },
          {
            value: 'floating',
            label: '浮动',
            icon: IconSidebarFloating,
          },
          {
            value: 'sidebar',
            label: '标准',
            icon: IconSidebarSidebar,
          },
        ].map((item) => (
          <RadioGroupItem key={item.value} item={item} />
        ))}
      </RadioGroup.Root>
      <div id='sidebar-description' className='sr-only'>
        在内嵌、浮动或标准侧边栏布局之间选择
      </div>
    </div>
  )
}

// 布局配置
function LayoutConfig() {
  const { open, setOpen } = useSidebar()
  const { defaultCollapsible, collapsible, setCollapsible } = useLayout()

  const radioState = open ? 'default' : collapsible

  return (
    <div className='max-md:hidden'>
      <SectionTitle
        title='布局'
        showReset={radioState !== 'default'}
        onReset={() => {
          setOpen(true)
          setCollapsible(defaultCollapsible)
        }}
      />
      <RadioGroup.Root
        value={radioState}
        onValueChange={(v) => {
          if (v === 'default') {
            setOpen(true)
            return
          }
          setOpen(false)
          setCollapsible(v as Collapsible)
        }}
        className='grid w-full max-w-md grid-cols-3 gap-4'
        aria-label='选择布局样式'
        aria-describedby='layout-description'
      >
        {[
          {
            value: 'default',
            label: '默认',
            icon: IconLayoutDefault,
          },
          {
            value: 'icon',
            label: '紧凑',
            icon: IconLayoutCompact,
          },
          {
            value: 'offcanvas',
            label: '完整',
            icon: IconLayoutFull,
          },
        ].map((item) => (
          <RadioGroupItem key={item.value} item={item} />
        ))}
      </RadioGroup.Root>
      <div id='layout-description' className='sr-only'>
        在默认展开、紧凑图标或完整布局模式之间选择
      </div>
    </div>
  )
}
