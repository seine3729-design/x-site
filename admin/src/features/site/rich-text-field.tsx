import { useEffect, useRef, useState } from 'react'
import type { Control, FieldPath, FieldValues } from 'react-hook-form'
import {
  Bold,
  Italic,
  Link2,
  List,
  ListOrdered,
  Quote,
  Redo2,
  RemoveFormatting,
  Underline,
  Undo2,
} from 'lucide-react'
import { Button, Separator } from '@/components/ui'
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { SelectDropdown } from '@/components/select-dropdown'

/**
 * 静态页正文的富文本编辑器（docs/06 第 13 节）。
 *
 * 不引入编辑器依赖：`admin/node_modules` 是宿主机与容器共用的同一份，装包会把另一侧的平台产物弄坏，
 * 而这里需要的能力（标题、加粗、列表、引用、链接）用 contentEditable + execCommand 就够。
 * execCommand 虽然标为废弃，但所有浏览器都支持，且是 contentEditable 排版唯一的通用接口。
 * 产出的 HTML 仍由后端按白名单清洗，所以这里不负责安全，只负责好用。
 */

type Block = 'p' | 'h2' | 'h3' | 'h4'

const BLOCK_ITEMS: { value: Block; label: string }[] = [
  { value: 'p', label: '正文' },
  { value: 'h2', label: '一级标题' },
  { value: 'h3', label: '二级标题' },
  { value: 'h4', label: '三级标题' },
]

/** execCommand 的封装：先把焦点放回编辑区，否则命令作用不到选区上。 */
function exec(command: string, value?: string) {
  document.execCommand(command, false, value)
}

function ToolButton({
  label,
  onClick,
  children,
  disabled,
}: {
  label: string
  onClick: () => void
  children: React.ReactNode
  disabled?: boolean
}) {
  return (
    <Button
      type='button'
      variant='ghost'
      size='icon'
      className='size-8'
      aria-label={label}
      title={label}
      disabled={disabled}
      // 用 mousedown 阻止默认行为，避免点按钮时编辑区失去选区
      onMouseDown={(e) => e.preventDefault()}
      onClick={onClick}
    >
      {children}
    </Button>
  )
}

export function RichTextField<T extends FieldValues>({
  control,
  name,
  label,
  description,
  required,
  disabled,
}: {
  control: Control<T>
  name: FieldPath<T>
  label: string
  description?: React.ReactNode
  required?: boolean
  disabled?: boolean
}) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>
            {label}
            {required && (
              <span className='text-destructive' aria-hidden>
                {' '}
                *
              </span>
            )}
          </FormLabel>
          <FormControl>
            <RichTextEditor value={field.value ?? ''} onChange={field.onChange} disabled={disabled} label={label} />
          </FormControl>
          {description && <FormDescription>{description}</FormDescription>}
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function RichTextEditor({
  value,
  onChange,
  disabled,
  label,
}: {
  value: string
  onChange: (html: string) => void
  disabled?: boolean
  label: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  const [block, setBlock] = useState<Block>('p')
  // 记住编辑区里的选区：点段落样式下拉时焦点会跑到弹层上、选区丢掉，execCommand 就作用不到任何内容上
  const savedRange = useRef<Range | null>(null)

  // 只在外部值与编辑区现有内容不一致时写回 DOM：每次渲染都写会把光标顶到开头
  useEffect(() => {
    const el = ref.current
    if (el && el.innerHTML !== value) el.innerHTML = value
  }, [value])

  // 让加粗/斜体产出 <b>/<i> 标签而不是 style 属性（后端白名单会去掉 style）；回车产出 <p> 而不是 <div>
  useEffect(() => {
    document.execCommand('styleWithCSS', false, 'false')
    document.execCommand('defaultParagraphSeparator', false, 'p')
  }, [])

  // 选区一变就记下来（鼠标拖选、键盘选择、全选都算），不依赖某个具体事件
  useEffect(() => {
    const onSelectionChange = () => {
      const el = ref.current
      const sel = document.getSelection()
      if (!el || !sel || sel.rangeCount === 0) return
      const range = sel.getRangeAt(0)
      if (el.contains(range.commonAncestorContainer)) savedRange.current = range.cloneRange()
    }
    document.addEventListener('selectionchange', onSelectionChange)
    return () => document.removeEventListener('selectionchange', onSelectionChange)
  }, [])

  const emit = () => {
    const el = ref.current
    if (el) onChange(el.innerHTML)
  }

  /** 记下当前选区（只记落在编辑区里的） */
  const remember = () => {
    const el = ref.current
    const sel = document.getSelection()
    if (!el || !sel || sel.rangeCount === 0) return
    const range = sel.getRangeAt(0)
    if (el.contains(range.commonAncestorContainer)) savedRange.current = range.cloneRange()
  }

  /** 把焦点和选区放回编辑区，execCommand 才有作用对象 */
  const restore = () => {
    const el = ref.current
    if (!el) return
    el.focus()
    const range = savedRange.current
    if (range && el.contains(range.commonAncestorContainer)) {
      const sel = document.getSelection()
      sel?.removeAllRanges()
      sel?.addRange(range)
    }
  }

  const run = (command: string, commandValue?: string) => {
    restore()
    exec(command, commandValue)
    emit()
    remember()
  }

  const BLOCK_TAGS = /^(?:P|H1|H2|H3|H4|H5|H6|UL|OL|BLOCKQUOTE|PRE|DIV|TABLE)$/

  /**
   * 空编辑器里直接打的字是裸文本节点，没有块级容器，formatBlock 无从转换（实测点「一级标题」没反应）。
   * 这里先把顶层的行内内容包成一个 <p>，并把选区落在它上面。
   */
  const ensureParagraph = () => {
    const el = ref.current
    if (!el || el.childNodes.length === 0) return
    if ([...el.children].some((child) => BLOCK_TAGS.test(child.tagName))) return
    const p = document.createElement('p')
    while (el.firstChild) p.appendChild(el.firstChild)
    el.appendChild(p)
    const range = document.createRange()
    range.selectNodeContents(p)
    const sel = document.getSelection()
    sel?.removeAllRanges()
    sel?.addRange(range)
    savedRange.current = range.cloneRange()
  }

  const applyBlock = (next: Block) => {
    setBlock(next)
    restore()
    ensureParagraph()
    exec('formatBlock', next.toUpperCase())
    emit()
    remember()
  }

  const addLink = () => {
    const url = window.prompt('请输入链接地址（必须以 http:// 或 https:// 开头）')
    if (!url) return
    if (!/^https?:\/\//i.test(url)) {
      window.alert('链接必须以 http:// 或 https:// 开头')
      return
    }
    run('createLink', url)
  }

  /** 跟随光标更新「当前段落样式」下拉的显示值，并记住选区 */
  const syncBlock = () => {
    remember()
    const current = document.queryCommandValue('formatBlock').toLowerCase()
    setBlock(current === 'h2' || current === 'h3' || current === 'h4' ? current : 'p')
  }

  return (
    <div className='overflow-hidden rounded-md border'>
      <div className='flex flex-wrap items-center gap-0.5 border-b bg-muted/50 p-1' role='toolbar' aria-label={`${label}排版工具`}>
        <div className='w-28'>
          <SelectDropdown
            isControlled
            defaultValue={block}
            onValueChange={(v) => applyBlock(v as Block)}
            placeholder='请选择段落样式'
            disabled={disabled}
            items={BLOCK_ITEMS}
          />
        </div>
        <Separator orientation='vertical' className='mx-1 h-6' />
        <ToolButton label='加粗' disabled={disabled} onClick={() => run('bold')}>
          <Bold className='size-4' />
        </ToolButton>
        <ToolButton label='斜体' disabled={disabled} onClick={() => run('italic')}>
          <Italic className='size-4' />
        </ToolButton>
        <ToolButton label='下划线' disabled={disabled} onClick={() => run('underline')}>
          <Underline className='size-4' />
        </ToolButton>
        <Separator orientation='vertical' className='mx-1 h-6' />
        <ToolButton label='无序列表' disabled={disabled} onClick={() => run('insertUnorderedList')}>
          <List className='size-4' />
        </ToolButton>
        <ToolButton label='有序列表' disabled={disabled} onClick={() => run('insertOrderedList')}>
          <ListOrdered className='size-4' />
        </ToolButton>
        <ToolButton label='引用' disabled={disabled} onClick={() => run('formatBlock', 'BLOCKQUOTE')}>
          <Quote className='size-4' />
        </ToolButton>
        <Separator orientation='vertical' className='mx-1 h-6' />
        <ToolButton label='插入链接' disabled={disabled} onClick={addLink}>
          <Link2 className='size-4' />
        </ToolButton>
        <ToolButton label='清除格式' disabled={disabled} onClick={() => run('removeFormat')}>
          <RemoveFormatting className='size-4' />
        </ToolButton>
        <Separator orientation='vertical' className='mx-1 h-6' />
        <ToolButton label='撤销' disabled={disabled} onClick={() => run('undo')}>
          <Undo2 className='size-4' />
        </ToolButton>
        <ToolButton label='重做' disabled={disabled} onClick={() => run('redo')}>
          <Redo2 className='size-4' />
        </ToolButton>
      </div>
      <div
        ref={ref}
        // 可编辑区：role=textbox + aria-multiline 让读屏知道这是多行输入（oxlint 要求可聚焦元素带显式 role）
        role='textbox'
        aria-multiline='true'
        aria-label={label}
        tabIndex={0}
        contentEditable={!disabled}
        suppressContentEditableWarning
        spellCheck={false}
        data-slot='rich-text-input'
        className='prose-admin max-h-104 min-h-56 overflow-y-auto px-3 py-2.5 text-sm outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50'
        onInput={() => {
          emit()
          remember()
        }}
        onBlur={() => {
          remember()
          emit()
        }}
        onKeyUp={syncBlock}
        onMouseUp={syncBlock}
        // 粘贴一律转成纯文本：从网页或 Word 粘进来的标签会被后端清洗掉，先转纯文本免得编辑时看到的和保存结果不一致
        onPaste={(event) => {
          event.preventDefault()
          const text = event.clipboardData.getData('text/plain')
          if (text) run('insertText', text)
        }}
      />
    </div>
  )
}
