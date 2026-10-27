import { useState } from 'react'
import type { ReactNode } from 'react'
import { useWatch, type Control, type FieldPath, type FieldValues } from 'react-hook-form'
import { ImageOff, ZoomIn } from 'lucide-react'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { TextField } from '@/features/content/components/fields'
import { buildUrl } from '@/lib/http'
import { cn } from '@/lib/utils'

const ABSOLUTE = /^https?:\/\//i

/**
 * 把后台保存的图片引用变成浏览器能加载的地址。
 *
 * 后台媒体字段存的是对象 key（如 `img/drama_cover/<uuid>.webp`），它既不是网址也不带存储源信息，
 * 直接塞进 `<img src>` 只会 404 —— 所以统一走同源预览代理 `GET /api/admin/images/preview`，
 * 由服务端从当前图片主存储读出来。运营手填的完整 http(s) 地址（白标 Logo、favicon、OG 图、表情）原样使用。
 *
 * 协议相对地址与 `data:` / `javascript:` 一律丢弃：这些值来自后台输入框，不能直接交给浏览器加载。
 */
export function imagePreviewUrl(value: string | null | undefined): string {
  const raw = (value ?? '').trim()
  if (!raw) return ''
  if (ABSOLUTE.test(raw)) return raw
  if (raw.startsWith('//') || /^[a-z][a-z0-9+.-]*:/i.test(raw)) return ''
  return buildUrl('/images/preview', { key: raw.replace(/^\/+/, '') })
}

/** 没有图或加载失败时的占位块，尺寸跟着缩略图走，避免布局跳动。 */
function Placeholder({ className, title }: { className?: string; title: string }) {
  return (
    <div
      className={cn('flex items-center justify-center rounded border bg-muted text-muted-foreground', className)}
      title={title}
      role='img'
      aria-label={title}
    >
      <ImageOff className='size-4' aria-hidden />
    </div>
  )
}

type ImageThumbProps = {
  /** 对象 key 或完整网址。 */
  value?: string | null
  /** 无障碍描述，同时作为放大弹窗的标题。 */
  alt: string
  /** 缩略图尺寸类名。默认 size-10，列表里用 h-12 w-9 这类贴合封面比例的值。 */
  className?: string
  /** 关掉点击放大。 */
  zoomable?: boolean
}

/**
 * 后台图片缩略图：点击放大查看原图，加载失败退化成占位块（坏链不会把表格撑乱）。
 * 缩略图尺寸由类名固定，图片本身 lazy 加载。
 */
export function ImageThumb({ value, alt, className = 'size-10', zoomable = true }: ImageThumbProps) {
  const [broken, setBroken] = useState(false)
  const [zoomed, setZoomed] = useState(false)
  const src = imagePreviewUrl(value)

  if (!src) return <Placeholder className={className} title={`${alt}：未设置`} />
  if (broken) return <Placeholder className={className} title={`${alt}：图片加载失败`} />

  const image = (
    <img
      src={src}
      alt={alt}
      loading='lazy'
      decoding='async'
      className={cn('rounded border bg-muted object-cover', className)}
      onError={() => setBroken(true)}
    />
  )
  if (!zoomable) return image

  return (
    <>
      <button
        type='button'
        aria-label={`查看${alt}大图`}
        onClick={() => setZoomed(true)}
        className='group relative shrink-0 rounded focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none'
      >
        {image}
        <span className='absolute inset-0 hidden items-center justify-center rounded bg-black/40 text-white group-hover:flex group-focus-visible:flex'>
          <ZoomIn className='size-4' aria-hidden />
        </span>
      </button>
      <ImageLightbox open={zoomed} onClose={() => setZoomed(false)} src={src} alt={alt} value={value ?? ''} />
    </>
  )
}

/** 放大查看：原图按窗口缩放显示，底部给出实际引用值方便运营核对。 */
function ImageLightbox({
  open,
  onClose,
  src,
  alt,
  value,
}: {
  open: boolean
  onClose: () => void
  src: string
  alt: string
  value: string
}) {
  if (!open) return null
  return (
    <Dialog open onOpenChange={(next) => !next && onClose()}>
      <DialogContent className='sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>{alt}</DialogTitle>
          <DialogDescription>按 Esc 或点击遮罩关闭。</DialogDescription>
        </DialogHeader>
        <img src={src} alt={alt} className='max-h-[70vh] w-full rounded border bg-muted object-contain' />
        <code className='max-w-full overflow-hidden text-ellipsis whitespace-nowrap rounded bg-muted px-2 py-1 text-xs'>{value}</code>
      </DialogContent>
    </Dialog>
  )
}

/**
 * 手填图片引用的字段：输入框 + 跟着输入实时更新的缩略图。
 *
 * 白标 Logo、favicon、默认分享图这些字段既可能填对象 key、也可能填外部完整地址，所以没走上传组件；
 * 但运营同样需要确认填进去的到底是哪张图、是不是坏链。
 */
export function ImageRefField<T extends FieldValues>({
  control,
  name,
  label,
  placeholder,
  description,
  disabled,
  required,
  thumbClassName = 'h-12 w-auto max-w-40 object-contain',
}: {
  control: Control<T>
  name: FieldPath<T>
  label: string
  placeholder: string
  description?: ReactNode
  disabled?: boolean
  required?: boolean
  thumbClassName?: string
}) {
  const value = useWatch({ control, name })
  return (
    <div className='flex flex-col gap-2'>
      <TextField
        control={control}
        name={name}
        label={label}
        placeholder={placeholder}
        description={description}
        disabled={disabled}
        required={required}
      />
      <ImageThumb value={typeof value === 'string' ? value : ''} alt={label} className={thumbClassName} />
    </div>
  )
}
