import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleCheck, CircleDashed, CircleOff, FolderPlus, ListChecks, Tags, X } from 'lucide-react'
import { toast } from 'sonner'
import { http } from '@/lib/http'
import { useCan } from '@/stores/auth-store'
import {
  Badge,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Label,
} from '@/components/ui'
import { WithReason } from '@/features/access/shared'
import { contentApi, contentKeys, taxonomiesQuery, type BulkAction, type BulkInput, type Page, type Topic } from './api'
import { SearchPicker, type PickerOption } from './components/search-picker'
import { TermsSelector } from './components/terms-selector'
import { searchTags } from './tags-page'
import { lookupReason } from './utils'

const ACTION_LABELS: Record<BulkAction, string> = {
  publish: '上架',
  offline: '下架',
  draft: '转草稿',
  add_tags: '打标签',
  add_to_topic: '加入专题',
  set_terms: '设维度取值',
}

/** 搜索手选专题（只有手选专题可以批量加入）。 */
async function searchManualTopics(q: string): Promise<PickerOption[]> {
  const page = await http.get<Page<Topic>>('/topics', { query: { q: q || undefined, source_type: 'manual', page_size: 20 } })
  return page.list.map((t) => ({ value: t.id, label: t.title, hint: `#${t.id} · ${t.slug}` }))
}

function useBulk(onDone: () => void) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: BulkInput) => contentApi.bulkDramas(body),
    onSuccess: (res, body) => {
      const skipped = body.ids.length - res.affected
      toast.success(
        `${ACTION_LABELS[body.action]}：${res.affected} 部剧有变化${skipped > 0 ? `，${skipped} 部无需处理（已是该状态、已有该标签或已在专题中）` : ''}`
      )
      void qc.invalidateQueries({ queryKey: contentKeys.dramas })
      if (body.action === 'add_to_topic') void qc.invalidateQueries({ queryKey: contentKeys.topics })
      onDone()
    },
  })
}

/** 选中剧后显示的批量操作栏（docs/06 第 4 节）。 */
export function BulkBar({ ids, onClear }: { ids: number[]; onClear: () => void }) {
  const can = useCan()
  const [dialog, setDialog] = useState<'add_tags' | 'add_to_topic' | 'set_terms' | null>(null)
  const bulk = useBulk(onClear)
  const tagsReason = lookupReason(can, 'tags')
  const topicsReason = lookupReason(can, 'topics')
  const termsReason = lookupReason(can, 'taxonomies')
  if (ids.length === 0) return null
  const run = (action: 'publish' | 'offline' | 'draft') => bulk.mutate({ ids, action })

  return (
    <>
      <div
        role='toolbar'
        aria-label={`对已选的 ${ids.length} 部剧批量操作`}
        className='sticky bottom-4 z-20 flex flex-wrap items-center gap-2 rounded-lg border bg-background/95 p-2 shadow-lg backdrop-blur'
      >
        <Badge>{ids.length}</Badge>
        <span className='text-sm'>部已选（本页）</span>
        <span className='mx-1 h-5 w-px bg-border' aria-hidden />
        <Button size='sm' variant='outline' disabled={bulk.isPending} onClick={() => run('publish')}>
          <CircleCheck className='size-4' /> 上架
        </Button>
        <Button size='sm' variant='outline' disabled={bulk.isPending} onClick={() => run('offline')}>
          <CircleOff className='size-4' /> 下架
        </Button>
        <Button size='sm' variant='outline' disabled={bulk.isPending} onClick={() => run('draft')}>
          <CircleDashed className='size-4' /> 转草稿
        </Button>
        {/* 这三项要搜索标签、专题或列出维度：没有对应页面的查看权限时禁用 */}
        <WithReason reason={tagsReason}>
          <Button size='sm' variant='outline' disabled={bulk.isPending || !!tagsReason} onClick={() => setDialog('add_tags')}>
            <Tags className='size-4' /> 打标签…
          </Button>
        </WithReason>
        <WithReason reason={topicsReason}>
          <Button size='sm' variant='outline' disabled={bulk.isPending || !!topicsReason} onClick={() => setDialog('add_to_topic')}>
            <FolderPlus className='size-4' /> 加入专题…
          </Button>
        </WithReason>
        <WithReason reason={termsReason}>
          <Button size='sm' variant='outline' disabled={bulk.isPending || !!termsReason} onClick={() => setDialog('set_terms')}>
            <ListChecks className='size-4' /> 设维度取值…
          </Button>
        </WithReason>
        <Button size='sm' variant='ghost' className='ms-auto' onClick={onClear}>
          <X className='size-4' /> 取消选择
        </Button>
      </div>
      {dialog === 'add_tags' && <AddTagsDialog ids={ids} onClose={() => setDialog(null)} onDone={onClear} />}
      {dialog === 'add_to_topic' && <AddToTopicDialog ids={ids} onClose={() => setDialog(null)} onDone={onClear} />}
      {dialog === 'set_terms' && <SetTermsDialog ids={ids} onClose={() => setDialog(null)} onDone={onClear} />}
    </>
  )
}

type BulkDialogProps = { ids: number[]; onClose: () => void; onDone: () => void }

function Footer({ onClose, disabled, pending, label }: { onClose: () => void; disabled: boolean; pending: boolean; label: string }) {
  return (
    <DialogFooter>
      <Button type='button' variant='outline' onClick={onClose}>
        取消
      </Button>
      <Button type='submit' disabled={disabled || pending}>
        {pending ? '处理中…' : label}
      </Button>
    </DialogFooter>
  )
}

function AddTagsDialog({ ids, onClose, onDone }: BulkDialogProps) {
  const [tags, setTags] = useState<PickerOption[]>([])
  const bulk = useBulk(() => {
    onDone()
    onClose()
  })
  const toggle = (o: PickerOption) =>
    setTags((cur) => (cur.some((t) => t.value === o.value) ? cur.filter((t) => t.value !== o.value) : [...cur, o]))
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <form
          className='flex flex-col gap-4'
          onSubmit={(e) => {
            e.preventDefault()
            bulk.mutate({ ids, action: 'add_tags', tag_ids: tags.map((t) => t.value) })
          }}
        >
          <DialogHeader>
            <DialogTitle>给 {ids.length} 部剧打标签</DialogTitle>
            <DialogDescription>追加所选标签，剧原有的标签保留。</DialogDescription>
          </DialogHeader>
          <div className='flex flex-col gap-2'>
            <Label>标签</Label>
            <SearchPicker
              queryKey={contentKeys.tags}
              search={(q) => searchTags(q)}
              selected={tags.map((t) => t.value)}
              onSelect={toggle}
              multiple
              trigger={tags.length ? `已选 ${tags.length} 个标签` : '搜索并选择标签（可多选）'}
              placeholder='搜名称或 slug'
              ariaLabel='选择标签'
            />
            {tags.length > 0 && (
              <div className='flex flex-wrap gap-1'>
                {tags.map((t) => (
                  <Badge key={t.value} variant='secondary' className='gap-1'>
                    {t.label}
                    <button type='button' aria-label={`移除 ${t.label}`} onClick={() => toggle(t)}>
                      <X className='size-3' />
                    </button>
                  </Badge>
                ))}
              </div>
            )}
          </div>
          <Footer onClose={onClose} disabled={tags.length === 0} pending={bulk.isPending} label='打标签' />
        </form>
      </DialogContent>
    </Dialog>
  )
}

function AddToTopicDialog({ ids, onClose, onDone }: BulkDialogProps) {
  const [topic, setTopic] = useState<PickerOption | null>(null)
  const bulk = useBulk(() => {
    onDone()
    onClose()
  })
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='sm:max-w-md'>
        <form
          className='flex flex-col gap-4'
          onSubmit={(e) => {
            e.preventDefault()
            if (topic) bulk.mutate({ ids, action: 'add_to_topic', topic_id: topic.value })
          }}
        >
          <DialogHeader>
            <DialogTitle>把 {ids.length} 部剧加入专题</DialogTitle>
            <DialogDescription>只能加入手选专题；按列表顺序追加在专题末尾，已在专题中的剧不动。</DialogDescription>
          </DialogHeader>
          <div className='flex flex-col gap-2'>
            <Label>专题</Label>
            <SearchPicker
              queryKey={contentKeys.topics}
              search={searchManualTopics}
              selected={topic ? [topic.value] : []}
              onSelect={setTopic}
              trigger={topic ? topic.label : '搜索并选择手选专题'}
              placeholder='搜标题或 slug'
              emptyText='没有找到手选专题'
              ariaLabel='选择专题'
            />
          </div>
          <Footer onClose={onClose} disabled={!topic} pending={bulk.isPending} label='加入专题' />
        </form>
      </DialogContent>
    </Dialog>
  )
}

function SetTermsDialog({ ids, onClose, onDone }: BulkDialogProps) {
  const taxonomies = useQuery(taxonomiesQuery)
  const [terms, setTerms] = useState<number[]>([])
  const [query, setQuery] = useState('')
  const bulk = useBulk(() => {
    onDone()
    onClose()
  })
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
        <form
          className='flex flex-col gap-4'
          onSubmit={(e) => {
            e.preventDefault()
            bulk.mutate({ ids, action: 'set_terms', term_ids: terms })
          }}
        >
          <DialogHeader>
            <DialogTitle>给 {ids.length} 部剧设维度取值</DialogTitle>
            <DialogDescription>多选维度追加所选取值；单选维度用所选取值替换原来的取值。没有选择的维度保持不变。</DialogDescription>
          </DialogHeader>
          <Input placeholder='搜取值名称' value={query} onChange={(e) => setQuery(e.target.value)} aria-label='搜取值' />
          {taxonomies.isPending ? (
            <p className='py-6 text-center text-sm text-muted-foreground'>加载中…</p>
          ) : (taxonomies.data ?? []).length === 0 ? (
            <p className='py-6 text-center text-sm text-muted-foreground'>还没有筛选维度，先到「筛选维度」中新建。</p>
          ) : (
            <TermsSelector taxonomies={taxonomies.data ?? []} value={terms} onChange={setTerms} query={query} noneLabel='不改' />
          )}
          <Footer onClose={onClose} disabled={terms.length === 0} pending={bulk.isPending} label={`应用（${terms.length} 个取值）`} />
        </form>
      </DialogContent>
    </Dialog>
  )
}
