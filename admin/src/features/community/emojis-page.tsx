import { useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronRight, ImageOff, Pencil, Plus, Trash2 } from 'lucide-react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { useCan } from '@/stores/auth-store'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { NoPermission } from '@/components/module-placeholder'
import {
  Badge,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Form } from '@/components/ui/form'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { TextField } from '@/features/content/components/fields'
import { type Emoji, type EmojiPack, emojiApi, emojiKeys, emojiListQuery } from './api'

type Dialog =
  | { type: 'pack-create' }
  | { type: 'pack-edit'; pack: EmojiPack }
  | { type: 'pack-delete'; pack: EmojiPack }
  | { type: 'emoji-create'; pack: EmojiPack }
  | { type: 'emoji-edit'; emoji: Emoji }
  | { type: 'emoji-delete'; emoji: Emoji }

/** 表情管理（docs/06「表情/表情包管理」）：维护表情包与表情，供前台评论 / 弹幕输入框下发。表情码形如 [微笑]，全局唯一。 */
export function EmojisPage() {
  const can = useCan()
  const visible = can('emojis.view')
  const list = useQuery({ ...emojiListQuery(), enabled: visible })
  const [dialog, setDialog] = useState<Dialog | null>(null)

  if (!visible) return <NoPermission title='表情管理' />
  const editable = can('emojis.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'
  const packs = list.data?.packs ?? []

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='表情管理'
        description='维护评论 / 弹幕输入框里的表情。表情分表情包管理，表情码形如 [微笑]、全局唯一，会原样出现在评论文本里由前台替换成图片。改动后前台最多 1 天内生效（强缓存）。'
        readonly={!editable}
        actions={
          editable && (
            <Button onClick={() => setDialog({ type: 'pack-create' })}>
              <Plus /> 新建表情包
            </Button>
          )
        }
      />

      {list.isError ? (
        <LoadError onRetry={() => void list.refetch()} />
      ) : list.isPending ? (
        <div className='rounded-md border p-8 text-center text-sm text-muted-foreground'>加载中…</div>
      ) : packs.length === 0 ? (
        <div className='rounded-md border p-8 text-center text-sm text-muted-foreground'>
          还没有表情包：点右上角「新建表情包」
        </div>
      ) : (
        <div className='flex flex-col gap-3'>
          {packs.map((pack) => (
            <PackCard
              key={pack.id}
              pack={pack}
              editable={editable}
              readonlyReason={readonlyReason}
              onDialog={setDialog}
            />
          ))}
        </div>
      )}

      {dialog?.type === 'pack-create' && <PackDialog onClose={() => setDialog(null)} />}
      {dialog?.type === 'pack-edit' && <PackDialog pack={dialog.pack} onClose={() => setDialog(null)} />}
      {dialog?.type === 'pack-delete' && <DeletePackDialog pack={dialog.pack} onClose={() => setDialog(null)} />}
      {dialog?.type === 'emoji-create' && <EmojiDialog pack={dialog.pack} onClose={() => setDialog(null)} />}
      {dialog?.type === 'emoji-edit' && <EmojiDialog emoji={dialog.emoji} onClose={() => setDialog(null)} />}
      {dialog?.type === 'emoji-delete' && <DeleteEmojiDialog emoji={dialog.emoji} onClose={() => setDialog(null)} />}
    </div>
  )
}

function PackCard({
  pack,
  editable,
  readonlyReason,
  onDialog,
}: {
  pack: EmojiPack
  editable: boolean
  readonlyReason: string | null
  onDialog: (d: Dialog) => void
}) {
  const [open, setOpen] = useState(true)
  return (
    <Collapsible open={open} onOpenChange={setOpen} className='rounded-md border'>
      <div className='flex items-center gap-2 px-3 py-2'>
        <CollapsibleTrigger className='flex flex-1 items-center gap-2 text-left'>
          <ChevronRight className={`size-4 shrink-0 text-muted-foreground transition-transform ${open ? 'rotate-90' : ''}`} />
          <span className='font-medium'>{pack.name}</span>
          <Badge variant='secondary'>{pack.emojis.length} 个表情</Badge>
          <span className='text-xs text-muted-foreground'>排序 {pack.sort}</span>
        </CollapsibleTrigger>
        <div className='flex items-center gap-1'>
          {editable && (
            <Button variant='outline' size='sm' onClick={() => onDialog({ type: 'emoji-create', pack })}>
              <Plus className='size-4' /> 添加表情
            </Button>
          )}
          <WithReason reason={readonlyReason}>
            <Button variant='ghost' size='icon' disabled={!editable} aria-label={`编辑表情包「${pack.name}」`} onClick={() => onDialog({ type: 'pack-edit', pack })}>
              <Pencil className='size-4' />
            </Button>
          </WithReason>
          <WithReason reason={readonlyReason}>
            <Button variant='ghost' size='icon' disabled={!editable} aria-label={`删除表情包「${pack.name}」`} onClick={() => onDialog({ type: 'pack-delete', pack })}>
              <Trash2 className='size-4' />
            </Button>
          </WithReason>
        </div>
      </div>
      <CollapsibleContent>
        <div className='border-t px-3 py-3'>
          {pack.emojis.length === 0 ? (
            <p className='py-4 text-center text-sm text-muted-foreground'>这个表情包还没有表情{editable ? '：点上方「添加表情」' : ''}</p>
          ) : (
            <ul className='grid grid-cols-[repeat(auto-fill,minmax(180px,1fr))] gap-2'>
              {pack.emojis.map((emoji) => (
                <li key={emoji.id} className='flex items-center gap-2 rounded-md border px-2 py-2'>
                  <EmojiThumb image={emoji.image} code={emoji.code} />
                  <div className='min-w-0 flex-1'>
                    <div className='truncate font-mono text-sm'>{emoji.code}</div>
                    <div className='text-xs text-muted-foreground'>排序 {emoji.sort}</div>
                  </div>
                  <div className='flex items-center'>
                    <WithReason reason={readonlyReason}>
                      <Button variant='ghost' size='icon' disabled={!editable} aria-label={`编辑表情「${emoji.code}」`} onClick={() => onDialog({ type: 'emoji-edit', emoji })}>
                        <Pencil className='size-4' />
                      </Button>
                    </WithReason>
                    <WithReason reason={readonlyReason}>
                      <Button variant='ghost' size='icon' disabled={!editable} aria-label={`删除表情「${emoji.code}」`} onClick={() => onDialog({ type: 'emoji-delete', emoji })}>
                        <Trash2 className='size-4' />
                      </Button>
                    </WithReason>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

/** 表情缩略图：加载失败时显示占位图标，避免坏链撑乱布局。 */
function EmojiThumb({ image, code }: { image: string; code: string }) {
  const [broken, setBroken] = useState(false)
  if (broken || !image) {
    return (
      <div className='flex size-9 shrink-0 items-center justify-center rounded bg-muted text-muted-foreground'>
        <ImageOff className='size-4' />
      </div>
    )
  }
  return <img src={image} alt={code} className='size-9 shrink-0 rounded object-contain' onError={() => setBroken(true)} />
}

const packSchema = z.object({
  name: z.string().trim().min(1, '请输入表情包名称').max(32, '名称最多 32 个字'),
  sort: z.coerce.number({ message: '排序必须是数字' }).int('排序必须是整数').min(0, '排序不能为负').max(9999, '排序过大'),
})

function PackDialog({ pack, onClose }: { pack?: EmojiPack; onClose: () => void }) {
  const qc = useQueryClient()
  const editing = !!pack
  const form = useForm<z.input<typeof packSchema>, unknown, z.output<typeof packSchema>>({
    resolver: zodResolver(packSchema),
    defaultValues: { name: pack?.name ?? '', sort: pack?.sort ?? 0 },
  })
  const mutation = useMutation({
    mutationFn: (v: z.output<typeof packSchema>) => (editing ? emojiApi.updatePack(pack.id, v) : emojiApi.createPack(v)),
    onSuccess: (saved) => {
      toast.success(editing ? `已保存「${saved.name}」` : `已新建「${saved.name}」`)
      void qc.invalidateQueries({ queryKey: emojiKeys.all })
      onClose()
    },
  })
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{editing ? '编辑表情包' : '新建表情包'}</DialogTitle>
          <DialogDescription>表情包是表情的分组，前台按包分区展示。排序越小越靠前。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <TextField control={form.control} name='name' label='表情包名称' required placeholder='请输入表情包名称，如 默认表情' />
            <TextField control={form.control} name='sort' label='排序' inputMode='numeric' placeholder='请输入排序，数字越小越靠前' />
            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                取消
              </Button>
              <Button type='submit' disabled={mutation.isPending}>
                {mutation.isPending ? '保存中…' : '保存'}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

const emojiSchema = z.object({
  code: z
    .string()
    .trim()
    .min(3, '表情码须形如 [微笑]')
    .max(32, '表情码最多 32 个字')
    .regex(/^\[.+\]$/, '表情码须用中括号包起来，如 [微笑]'),
  image: z.string().trim().min(1, '请输入表情图片地址').max(500, '图片地址过长'),
  sort: z.coerce.number({ message: '排序必须是数字' }).int('排序必须是整数').min(0, '排序不能为负').max(9999, '排序过大'),
})

function EmojiDialog({ pack, emoji, onClose }: { pack?: EmojiPack; emoji?: Emoji; onClose: () => void }) {
  const qc = useQueryClient()
  const editing = !!emoji
  const form = useForm<z.input<typeof emojiSchema>, unknown, z.output<typeof emojiSchema>>({
    resolver: zodResolver(emojiSchema),
    defaultValues: { code: emoji?.code ?? '', image: emoji?.image ?? '', sort: emoji?.sort ?? 0 },
  })
  const mutation = useMutation({
    mutationFn: (v: z.output<typeof emojiSchema>) => (editing ? emojiApi.updateEmoji(emoji.id, v) : emojiApi.createEmoji(pack!.id, v)),
    onSuccess: () => {
      toast.success(editing ? '已保存表情' : '已添加表情')
      void qc.invalidateQueries({ queryKey: emojiKeys.all })
      onClose()
    },
  })
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{editing ? '编辑表情' : `往「${pack?.name}」添加表情`}</DialogTitle>
          <DialogDescription>表情码会原样出现在评论 / 弹幕文本里，由前台替换成图片；码全局唯一，重复会被拒绝。</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => mutation.mutate(v))} noValidate>
            <TextField control={form.control} name='code' label='表情码' required placeholder='请输入表情码，如 [微笑]' />
            <TextField
              control={form.control}
              name='image'
              label='图片地址'
              required
              placeholder='请输入图片地址，如 /img/emoji/smile.png'
              description='资源路径（/ 开头）或 http(s) 地址。'
            />
            <TextField control={form.control} name='sort' label='排序' inputMode='numeric' placeholder='请输入排序，数字越小越靠前' />
            <DialogFooter>
              <Button type='button' variant='outline' onClick={onClose}>
                取消
              </Button>
              <Button type='submit' disabled={mutation.isPending}>
                {mutation.isPending ? '保存中…' : '保存'}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

function DeletePackDialog({ pack, onClose }: { pack: EmojiPack; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => emojiApi.deletePack(pack.id),
    onSuccess: () => {
      toast.success(`已删除「${pack.name}」`)
      void qc.invalidateQueries({ queryKey: emojiKeys.all })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除「${pack.name}」`}
      desc={`删除后这个表情包连同里面的 ${pack.emojis.length} 个表情都会被移除，前台最多 1 天内不再下发。`}
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}

function DeleteEmojiDialog({ emoji, onClose }: { emoji: Emoji; onClose: () => void }) {
  const qc = useQueryClient()
  const mutation = useMutation({
    mutationFn: () => emojiApi.deleteEmoji(emoji.id),
    onSuccess: () => {
      toast.success(`已删除「${emoji.code}」`)
      void qc.invalidateQueries({ queryKey: emojiKeys.all })
      onClose()
    },
  })
  return (
    <ConfirmDialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={`删除「${emoji.code}」`}
      desc='删除后这个表情不再出现在前台输入框。'
      cancelBtnText='取消'
      confirmText={mutation.isPending ? '删除中…' : '删除'}
      destructive
      isLoading={mutation.isPending}
      handleConfirm={() => mutation.mutate()}
    />
  )
}
