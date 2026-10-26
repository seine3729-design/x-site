import { useEffect, useState } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Pin, RefreshCw, X } from 'lucide-react'
import { useFieldArray, useForm, useWatch } from 'react-hook-form'
import { toast } from 'sonner'
import { z } from 'zod'
import { formatDateTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useCan } from '@/stores/auth-store'
import { NoPermission } from '@/components/module-placeholder'
import { SelectDropdown } from '@/components/select-dropdown'
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Input,
  Label,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '@/components/ui'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { LoadError, PageHeading, WithReason } from '@/features/access/shared'
import { contentKeys, DRAMA_STATUS_LABELS } from '@/features/content/api'
import { TextField } from '@/features/content/components/fields'
import { SearchPicker, type PickerOption } from '@/features/content/components/search-picker'
import { searchDramas } from '@/features/content/drama-search'
import { lookupReason, parseIdList } from '@/features/content/utils'
import {
  MAX_RANK_OVERRIDES,
  MAX_RANK_WEIGHT,
  opsApi,
  opsKeys,
  RANK_BOARD_LABELS,
  RANK_BOARDS,
  rankBoardQuery,
  rankingConfigQuery,
  type RankBoard,
  type RankBoardView,
  type RankOverride,
  type RankRow,
  type RankWeights,
} from './api'
import {
  formatHeat,
  formToWeights,
  isRankBoost,
  isRankWeight,
  newRankOverrideRow,
  RANK_MODE_LABELS,
  RANK_MODES,
  rankDiffers,
  rankMove,
  rankOverrideToRow,
  rowToRankOverrideInput,
  weightsToForm,
  type RankMode,
  type RankWeightsForm,
} from './utils'

const DEFAULT_BOARD: RankBoard = 'drama_week'

/** 排行干预（docs/06 第 21 节）：剧集周榜 / 月榜的人工调整与计分系数。改完要重新排名才在前台生效。 */
export function RanksPage() {
  const can = useCan()
  const visible = can('ranks.view')
  const search = useSearch({ from: '/_authenticated/ranks' })
  const navigate = useNavigate({ from: '/ranks' })
  const board = search.board ?? DEFAULT_BOARD
  const view = useQuery({ ...rankBoardQuery(board), enabled: visible })
  const weights = useQuery({ ...rankingConfigQuery(), enabled: visible })
  // 人工调整有未保存的修改时，切换榜单前先确认
  const [dirty, setDirty] = useState(false)
  const qc = useQueryClient()
  const recompute = useMutation({
    mutationFn: () => opsApi.recomputeRanks(),
    onSuccess: ({ done }) => {
      if (done) toast.success('已重新排名，前台已是新的榜单')
      else toast.info('已交给后台重新排名，稍后自动完成，不用重复点击')
      void qc.invalidateQueries({ queryKey: opsKeys.rankBoards })
    },
  })

  if (!visible) return <NoPermission title='排行干预' />
  const editable = can('ranks.edit')
  const readonlyReason = editable ? null : '当前角色只能查看'

  const switchBoard = (next: string) => {
    if (next === board) return
    if (dirty && !window.confirm('人工调整的修改尚未保存，确定切换榜单吗？')) return
    void navigate({ search: { board: next === DEFAULT_BOARD ? undefined : (next as RankBoard) } })
  }

  return (
    <div className='flex flex-col gap-4'>
      <PageHeading
        title='排行干预'
        description='剧集周榜、月榜各显示前 20 名。分数 = 播放、追剧、点赞、看完的次数 × 各自的系数 + 运营加分；后台每 5 分钟重新排名一次。修改人工调整或系数后，要重新排名才在前台生效。'
        readonly={!editable}
        actions={
          <WithReason reason={readonlyReason}>
            <Button disabled={!editable || recompute.isPending} onClick={() => recompute.mutate()}>
              <RefreshCw className={cn(recompute.isPending && 'animate-spin')} aria-hidden />
              {recompute.isPending ? '重新排名中…' : '马上重新排名'}
            </Button>
          </WithReason>
        }
      />

      {weights.isError ? (
        <LoadError onRetry={() => void weights.refetch()} />
      ) : weights.isPending ? (
        <p className='text-sm text-muted-foreground'>加载中…</p>
      ) : (
        <WeightsCard key={JSON.stringify(weights.data)} weights={weights.data} disabledReason={readonlyReason} />
      )}

      <Tabs value={board} onValueChange={switchBoard}>
        <TabsList>
          {RANK_BOARDS.map((b) => (
            <TabsTrigger key={b} value={b}>
              {RANK_BOARD_LABELS[b]}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent value={board} className='mt-2 flex flex-col gap-4'>
          {view.isError ? (
            <LoadError onRetry={() => void view.refetch()} />
          ) : view.isPending ? (
            <p className='py-8 text-center text-sm text-muted-foreground'>加载中…</p>
          ) : (
            <>
              {/* 调整变了（保存后、别人改过）才重新载入表单；重新排名、改系数只刷新榜单，不丢正在编辑的内容 */}
              <OverridesCard
                key={overridesKey(view.data)}
                board={board}
                overrides={view.data.overrides}
                disabledReason={readonlyReason}
                onDirtyChange={setDirty}
              />
              <div className='grid gap-4 xl:grid-cols-2'>
                <LiveCard view={view.data} />
                <PreviewCard view={view.data} />
              </div>
            </>
          )}
        </TabsContent>
      </Tabs>
    </div>
  )
}

const overridesKey = (v: RankBoardView) => `${v.board}:${v.overrides.map((o) => `${o.drama_id}@${o.updated_at}`).join(',')}`

// ---------- 计分系数 ----------

const weightField = z.string().refine(isRankWeight, `请输入 0–${MAX_RANK_WEIGHT} 的数字，最多两位小数`)
const weightsSchema = z.object({ play: weightField, follow: weightField, like: weightField, finish: weightField })

const WEIGHT_FIELDS: { name: keyof RankWeightsForm; label: string; placeholder: string }[] = [
  { name: 'play', label: '播放一次', placeholder: '0–1000，默认 1' },
  { name: 'follow', label: '追剧一次', placeholder: '0–1000，默认 3' },
  { name: 'like', label: '点赞一次', placeholder: '0–1000，默认 2' },
  { name: 'finish', label: '看完一集', placeholder: '0–1000，默认 2' },
]

function WeightsCard({ weights, disabledReason }: { weights: RankWeights; disabledReason: string | null }) {
  const qc = useQueryClient()
  const form = useForm<z.input<typeof weightsSchema>, unknown, RankWeightsForm>({
    resolver: zodResolver(weightsSchema),
    defaultValues: weightsToForm(weights),
  })
  const save = useMutation({
    mutationFn: (f: RankWeightsForm) => opsApi.setRankingConfig(formToWeights(f)),
    onSuccess: (saved) => {
      qc.setQueryData(opsKeys.rankingConfig, saved)
      // 预览按新系数现算
      void qc.invalidateQueries({ queryKey: opsKeys.rankBoards })
      toast.success('已保存计分系数，重新排名后前台生效')
    },
  })
  const dirty = form.formState.isDirty
  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>计分系数</CardTitle>
        <CardDescription>两张榜共用，每项 0–1000、最多两位小数。追剧、点赞、看完的数据接入前都按 0 次计，现在只有播放影响排名。</CardDescription>
      </CardHeader>
      <CardContent>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => save.mutate(v))} noValidate>
            <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
              {WEIGHT_FIELDS.map((f) => (
                <TextField
                  key={f.name}
                  control={form.control}
                  name={f.name}
                  label={f.label}
                  required
                  inputMode='decimal'
                  placeholder={f.placeholder}
                  disabled={!!disabledReason}
                />
              ))}
            </div>
            <div className='flex flex-wrap items-center justify-end gap-2'>
              <Button type='button' variant='outline' disabled={!dirty || save.isPending} onClick={() => form.reset()}>
                还原
              </Button>
              <WithReason reason={disabledReason}>
                <Button type='submit' disabled={!!disabledReason || !dirty || save.isPending}>
                  {save.isPending ? '保存中…' : '保存系数'}
                </Button>
              </WithReason>
            </div>
          </form>
        </Form>
      </CardContent>
    </Card>
  )
}

// ---------- 人工调整 ----------

const overrideRowSchema = z
  .object({
    drama_id: z.number(),
    title: z.string(),
    drama_status: z.enum(['draft', 'published', 'offline']).nullable(),
    in_trash: z.boolean(),
    mode: z.enum(RANK_MODES),
    boost: z.string(),
  })
  .superRefine((r, ctx) => {
    if (r.mode === 'blocked') return
    const s = r.boost.trim()
    if (s !== '' && !isRankBoost(s)) {
      ctx.addIssue({ code: 'custom', path: ['boost'], message: '请输入整数，绝对值不超过 10 亿' })
    } else if (r.mode === 'boost' && (s === '' || Number(s) === 0)) {
      ctx.addIssue({ code: 'custom', path: ['boost'], message: '请填写不为 0 的整数，负数为减分' })
    }
  })

const overridesSchema = z.object({
  rows: z.array(overrideRowSchema).max(MAX_RANK_OVERRIDES, `每张榜最多调整 ${MAX_RANK_OVERRIDES} 部剧`),
})
type OverridesForm = z.output<typeof overridesSchema>

const BOOST_PLACEHOLDERS: Record<RankMode, string> = {
  pinned: '选填，如 500',
  blocked: '不让上榜时不计分',
  boost: '如 500，减分填 -200',
}

function OverridesCard({
  board,
  overrides,
  disabledReason,
  onDirtyChange,
}: {
  board: RankBoard
  overrides: RankOverride[]
  disabledReason: string | null
  onDirtyChange: (dirty: boolean) => void
}) {
  const qc = useQueryClient()
  const dramasReason = lookupReason(useCan(), 'dramas')
  const form = useForm<z.input<typeof overridesSchema>, unknown, OverridesForm>({
    resolver: zodResolver(overridesSchema),
    defaultValues: { rows: overrides.map(rankOverrideToRow) },
  })
  const rows = useFieldArray({ control: form.control, name: 'rows' })
  const values = useWatch({ control: form.control, name: 'rows' })
  const dirty = form.formState.isDirty
  useEffect(() => onDirtyChange(dirty), [dirty, onDirtyChange])
  useEffect(() => () => onDirtyChange(false), [onDirtyChange])
  const [paste, setPaste] = useState('')
  const pasted = parseIdList(paste)
  const save = useMutation({
    mutationFn: (v: OverridesForm) => opsApi.setRankOverrides(board, v.rows.map(rowToRankOverrideInput)),
    onSuccess: (saved) => {
      qc.setQueryData(opsKeys.rankBoard(board), saved)
      toast.success(`已保存${RANK_BOARD_LABELS[board]}的人工调整（${saved.overrides.length} 部剧），重新排名后前台生效`)
    },
  })

  const editable = !disabledReason
  const full = rows.fields.length >= MAX_RANK_OVERRIDES
  const fullReason = full ? `每张榜最多调整 ${MAX_RANK_OVERRIDES} 部剧` : null
  const add = (items: { id: number; title?: string }[]) => {
    const have = new Set(form.getValues('rows').map((r) => r.drama_id))
    const fresh = items.filter((d) => !have.has(d.id))
    if (fresh.length < items.length) toast.info('已在调整列表里的剧不会重复添加')
    const room = MAX_RANK_OVERRIDES - have.size
    if (fresh.length > room) toast.error(`每张榜最多调整 ${MAX_RANK_OVERRIDES} 部剧，多出的没有添加`)
    const list = fresh.slice(0, Math.max(room, 0)).map((d) => newRankOverrideRow(d.id, d.title))
    if (list.length > 0) rows.append(list)
  }
  const addPasted = () => {
    if (pasted.ids.length === 0) return
    add(pasted.ids.map((id) => ({ id })))
    setPaste('')
  }
  const pasteId = `rank-paste-${board}`

  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>
          人工调整（{rows.fields.length} / {MAX_RANK_OVERRIDES}）
        </CardTitle>
        <CardDescription>
          只影响{RANK_BOARD_LABELS[board]}
          。置顶的剧排在最前（多部置顶之间按分数，可以再加减分调整先后）；不让上榜的剧不出现；加分直接加到分数上，负数为减分。
          剧不在前台显示（未上架、已下架、在回收站）时不上榜，调整保留。保存后要重新排名才生效。
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Form {...form}>
          <form className='flex flex-col gap-4' onSubmit={form.handleSubmit((v) => save.mutate(v))} noValidate>
            {editable && (
              <div className='grid gap-3 sm:grid-cols-2'>
                <div className='flex flex-col gap-2'>
                  <Label>按剧名添加</Label>
                  {/* 搜剧要用「剧集」页面的读接口；没有查看权限时仍可按编号添加 */}
                  <WithReason reason={dramasReason ?? fullReason}>
                    <SearchPicker
                      queryKey={contentKeys.dramas}
                      search={searchDramas}
                      selected={values.map((r) => r.drama_id)}
                      onSelect={(o: PickerOption) => add([{ id: o.value, title: o.label }])}
                      trigger='搜索剧名、别名或 #编号'
                      placeholder='输入剧名或 #编号'
                      multiple
                      ariaLabel='搜索要调整的剧'
                      disabled={!!dramasReason || full}
                      // 与旁边的输入框等高
                      className='h-10 w-full'
                    />
                  </WithReason>
                </div>
                <div className='flex flex-col gap-2'>
                  <Label htmlFor={pasteId}>按编号添加</Label>
                  <div className='flex gap-2'>
                    <Input
                      id={pasteId}
                      autoComplete='off'
                      placeholder='请输入剧集编号，如 12, 15, #18'
                      value={paste}
                      onChange={(e) => setPaste(e.target.value)}
                      onKeyDown={(e) => {
                        // 回车是添加，不是提交整个表单
                        if (e.key === 'Enter') {
                          e.preventDefault()
                          addPasted()
                        }
                      }}
                    />
                    <WithReason reason={fullReason}>
                      <Button type='button' variant='secondary' className='h-10' disabled={pasted.ids.length === 0 || full} onClick={addPasted}>
                        添加
                      </Button>
                    </WithReason>
                  </div>
                  {pasted.invalid.length > 0 && <p className='text-xs text-destructive'>无法识别：{pasted.invalid.join('、')}</p>}
                </div>
              </div>
            )}

            {rows.fields.length === 0 ? (
              <p className='rounded-md border border-dashed py-8 text-center text-sm text-muted-foreground'>这张榜没有人工调整，全部按分数排名。</p>
            ) : (
              <div className='overflow-x-auto rounded-md border'>
                <Table>
                  <TableHeader>
                    <TableRow className='bg-muted'>
                      <TableHead className='pl-4'>剧集</TableHead>
                      <TableHead className='w-44'>调整方式</TableHead>
                      <TableHead className='w-52'>加分</TableHead>
                      <TableHead className='w-12 pr-4'>
                        <span className='sr-only'>操作</span>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {rows.fields.map((f, i) => {
                      const mode = values[i]?.mode ?? f.mode
                      const name = f.title || `#${f.drama_id}`
                      const hidden = f.in_trash
                        ? '在回收站'
                        : f.drama_status && f.drama_status !== 'published'
                          ? DRAMA_STATUS_LABELS[f.drama_status]
                          : null
                      return (
                        <TableRow key={f.id}>
                          <TableCell className='pl-4 align-top'>
                            <div className='flex flex-col gap-1 py-1.5'>
                              <span className='font-medium break-all'>{f.title || '（保存时校验是否存在）'}</span>
                              <div className='flex flex-wrap items-center gap-2 text-xs text-muted-foreground'>
                                <span className='tabular-nums'>#{f.drama_id}</span>
                                {hidden && <Badge variant='outline'>{hidden}，不上榜</Badge>}
                              </div>
                            </div>
                          </TableCell>
                          <TableCell className='align-top'>
                            <FormField
                              control={form.control}
                              name={`rows.${i}.mode`}
                              render={({ field }) => (
                                <FormItem>
                                  <FormLabel className='sr-only'>{name} 的调整方式</FormLabel>
                                  <SelectDropdown
                                    isControlled
                                    defaultValue={field.value}
                                    onValueChange={field.onChange}
                                    placeholder='请选择调整方式'
                                    disabled={!editable}
                                    items={RANK_MODES.map((m) => ({ value: m, label: RANK_MODE_LABELS[m] }))}
                                  />
                                  <FormMessage />
                                </FormItem>
                              )}
                            />
                          </TableCell>
                          <TableCell className='align-top'>
                            <FormField
                              control={form.control}
                              name={`rows.${i}.boost`}
                              render={({ field }) => (
                                <FormItem>
                                  <FormLabel className='sr-only'>{name} 的加分</FormLabel>
                                  <FormControl>
                                    <Input
                                      autoComplete='off'
                                      placeholder={BOOST_PLACEHOLDERS[mode]}
                                      disabled={!editable || mode === 'blocked'}
                                      {...field}
                                      // 不让上榜时不计分：清空显示，保存时按 0
                                      value={mode === 'blocked' ? '' : field.value}
                                    />
                                  </FormControl>
                                  <FormMessage />
                                </FormItem>
                              )}
                            />
                          </TableCell>
                          <TableCell className='pr-4 align-top'>
                            <WithReason reason={disabledReason}>
                              <Button
                                type='button'
                                variant='ghost'
                                size='icon'
                                disabled={!editable}
                                aria-label={`移出调整：${name}`}
                                onClick={() => rows.remove(i)}
                              >
                                <X className='size-4' />
                              </Button>
                            </WithReason>
                          </TableCell>
                        </TableRow>
                      )
                    })}
                  </TableBody>
                </Table>
              </div>
            )}

            <div className='flex flex-wrap items-center justify-end gap-2'>
              {dirty && <span className='me-auto text-sm text-muted-foreground'>有未保存的修改</span>}
              <Button type='button' variant='outline' disabled={!dirty || save.isPending} onClick={() => form.reset()}>
                还原
              </Button>
              <WithReason reason={disabledReason}>
                <Button type='submit' disabled={!editable || !dirty || save.isPending}>
                  {save.isPending ? '保存中…' : '保存调整'}
                </Button>
              </WithReason>
            </div>
          </form>
        </Form>
      </CardContent>
    </Card>
  )
}

// ---------- 线上榜单与预览 ----------

function LiveCard({ view }: { view: RankBoardView }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>线上榜单</CardTitle>
        <CardDescription>
          {view.computed_at
            ? `前台正在显示的前 20 名，排名时间 ${formatDateTime(view.computed_at)}。之后下架的剧在前台会去掉、名次顺延。`
            : '还没有排过名：前台第一次打开榜单时会自动排名，也可以点「马上重新排名」。'}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <RankTable rows={view.live} emptyText={view.computed_at ? '上次排名时没有可上榜的剧。' : '还没有排过名。'} />
      </CardContent>
    </Card>
  )
}

function PreviewCard({ view }: { view: RankBoardView }) {
  const differs = rankDiffers(view.live, view.preview)
  return (
    <Card>
      <CardHeader>
        <CardTitle className='text-base'>预览</CardTitle>
        <CardDescription>
          按已保存的系数与人工调整现算的前 20 名。
          {differs ? <span className='font-medium text-foreground'>与线上榜单不同，重新排名后前台就是这个结果。</span> : '与线上榜单一致。'}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <RankTable
          rows={view.preview}
          // 还没排过名时不比较名次变化
          live={view.computed_at ? view.live : undefined}
          emptyText='没有可上榜的剧：还没有前台可见的剧，或都设成了不让上榜。'
        />
      </CardContent>
    </Card>
  )
}

/** 名次表。传了 live 时多一列「变化」：相对线上榜单的名次升降。 */
function RankTable({ rows, live, emptyText }: { rows: RankRow[]; live?: RankRow[]; emptyText: string }) {
  if (rows.length === 0) {
    return <p className='rounded-md border border-dashed py-8 text-center text-sm text-muted-foreground'>{emptyText}</p>
  }
  return (
    <div className='overflow-x-auto rounded-md border'>
      <Table>
        <TableHeader>
          <TableRow className='bg-muted'>
            <TableHead className='w-14 pl-4 text-right'>名次</TableHead>
            <TableHead>剧集</TableHead>
            <TableHead className={cn('text-right', !live && 'pr-4')}>分数</TableHead>
            {live && <TableHead className='pr-4 text-right'>变化</TableHead>}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((r) => (
            <TableRow key={r.drama_id}>
              <TableCell className='pl-4 text-right tabular-nums'>{r.rank}</TableCell>
              <TableCell>
                <div className='flex flex-wrap items-center gap-x-2 gap-y-1'>
                  {r.pinned && (
                    <Badge variant='secondary'>
                      <Pin aria-hidden /> 置顶
                    </Badge>
                  )}
                  <span className='font-medium break-all'>{r.title || '（剧已删除）'}</span>
                  <span className='text-xs text-muted-foreground tabular-nums'>#{r.drama_id}</span>
                </div>
              </TableCell>
              <TableCell className={cn('text-right tabular-nums', !live && 'pr-4')}>{formatHeat(r.heat)}</TableCell>
              {live && (
                <TableCell className='pr-4 text-right whitespace-nowrap'>
                  <RankMoveText move={rankMove(live, r)} />
                </TableCell>
              )}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

/** 名次升降：箭头 + 数字，读屏软件读出完整说明（不只靠符号）。 */
function RankMoveText({ move }: { move: number | null }) {
  if (move === null) return <Badge variant='outline'>新上榜</Badge>
  if (move === 0) {
    return (
      <span className='text-muted-foreground'>
        <span aria-hidden>—</span>
        <span className='sr-only'>名次不变</span>
      </span>
    )
  }
  const up = move > 0
  return (
    <span className={cn('tabular-nums', up ? 'font-medium' : 'text-muted-foreground')}>
      <span aria-hidden>
        {up ? '↑' : '↓'}
        {Math.abs(move)}
      </span>
      <span className='sr-only'>
        {up ? '上升' : '下降'} {Math.abs(move)} 名
      </span>
    </span>
  )
}
