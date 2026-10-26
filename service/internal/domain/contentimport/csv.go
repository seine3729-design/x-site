package contentimport

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

// 列名的别名表：运营手里的 CSV 可能来自不同工具，表头写法五花八门，都认下来比让他改文件好。
// 比对前会把表头转小写、去空白与下划线。
var columnAliases = map[string]string{
	"剧名": colTitle, "名称": colTitle, "标题": colTitle, "title": colTitle, "name": colTitle,
	"别名": colAliases, "又名": colAliases, "alias": colAliases, "aliases": colAliases,
	"简介": colSummary, "描述": colSummary, "summary": colSummary, "description": colSummary, "desc": colSummary,
	"标签": colTags, "tag": colTags, "tags": colTags,
}

const (
	colTitle   = "title"
	colAliases = "aliases"
	colSummary = "summary"
	colTags    = "tags"
)

// multiSep 是别名与标签在单个单元格里的分隔符。
//
// 中英文逗号、顿号、竖线、分号都认：运营在 Excel 里打出来的分隔符取决于输入法，
// 只认一种的话必然有人填错而且看不出哪里错了。
var multiSep = func(r rune) bool {
	switch r {
	case ',', '，', '、', '|', ';', '；', '/':
		return true
	}
	return false
}

const (
	maxTitleRunes   = 200
	maxAliasRunes   = 100
	maxAliases      = 20
	maxSummaryRunes = 5000
	maxTagRunes     = 32
	maxTags         = 20
)

// ParseCSV 解析 CSV，返回清洗后的行。
//
// 要求第一行是表头：没有表头就无法知道哪一列是什么，按位置猜会在列顺序变化时静默导错数据。
func ParseCSV(data []byte) ([]Row, error) {
	if len(data) == 0 {
		return nil, badRequest("文件是空的")
	}
	if len(data) > MaxBytes {
		return nil, badRequest("文件不能超过 %d KB", MaxBytes/1024)
	}
	// Excel 存成 UTF-8 CSV 时会加 BOM，不去掉的话第一个列名会变成「\ufeff剧名」而认不出来
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(data) {
		return nil, badRequest("文件不是 UTF-8 编码。请在 Excel 里用「CSV UTF-8」格式另存一次")
	}

	r := csv.NewReader(bytes.NewReader(data))
	// 列数不固定：有的行末尾少几个逗号是常见情况，按表头对齐即可
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err == io.EOF {
		return nil, badRequest("文件是空的")
	}
	if err != nil {
		return nil, badRequest("第 1 行读不出来：%s", csvErrMsg(err))
	}
	idx, err := mapColumns(header)
	if err != nil {
		return nil, err
	}

	var rows []Row
	line := 1 // 表头占第 1 行
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		line++
		if err != nil {
			return nil, badRequest("第 %d 行读不出来：%s", line, csvErrMsg(err))
		}
		if len(rows) >= MaxRows {
			return nil, badRequest("一次最多导入 %d 行，请把文件拆开分批导入", MaxRows)
		}
		rows = append(rows, rowOf(line, rec, idx))
	}
	if len(rows) == 0 {
		return nil, badRequest("除了表头没有数据行")
	}
	return rows, nil
}

// mapColumns 把表头映射成列下标。剧名是唯一必需的列。
func mapColumns(header []string) (map[string]int, error) {
	idx := map[string]int{}
	for i, h := range header {
		if col, ok := columnAliases[normalizeHeader(h)]; ok {
			// 同一列出现多次时用第一个，不然后面的空列会覆盖掉有数据的
			if _, dup := idx[col]; !dup {
				idx[col] = i
			}
		}
	}
	if _, ok := idx[colTitle]; !ok {
		return nil, badRequest("表头里找不到「剧名」列。第一行需要是表头，列名可以是：剧名、别名、简介、标签")
	}
	return idx, nil
}

// normalizeHeader 归一化列名：去空白与下划线、转小写、去掉 BOM 残留。
func normalizeHeader(h string) string {
	h = strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))
	h = strings.NewReplacer(" ", "", "\t", "", "_", "", "-", "").Replace(h)
	return strings.ToLower(h)
}

// rowOf 取出一行的各列并清洗。超长的截断而不是报错：
// 为一行简介多了几个字就让整个文件导不进去，运营只能自己去数字数，不值得。
func rowOf(line int, rec []string, idx map[string]int) Row {
	at := func(col string) string {
		i, ok := idx[col]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	row := Row{
		Line:    line,
		Title:   truncRunes(at(colTitle), maxTitleRunes),
		Summary: truncRunes(at(colSummary), maxSummaryRunes),
	}
	row.Aliases = splitList(at(colAliases), maxAliasRunes, maxAliases)
	row.Tags = splitList(at(colTags), maxTagRunes, maxTags)
	// 别名里出现剧名本身是常见的，去掉它：别名字段的意义是「除了剧名之外还叫什么」
	row.Aliases = dropFold(row.Aliases, row.Title)
	return row
}

// splitList 按多种分隔符切分一个单元格，去空、去重（忽略大小写）、截断、限数量。
func splitList(s string, maxRunes, maxItems int) []string {
	if s == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(s, multiSep) {
		v := truncRunes(strings.TrimSpace(part), maxRunes)
		if v == "" {
			continue
		}
		k := strings.ToLower(v)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, v)
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

// dropFold 去掉与 v 相同（忽略大小写）的项。
func dropFold(list []string, v string) []string {
	if v == "" {
		return list
	}
	out := list[:0]
	for _, s := range list {
		if !strings.EqualFold(s, v) {
			out = append(out, s)
		}
	}
	return out
}

// truncRunes 按字符数截断（不是字节数，否则会把中文截断成乱码）。
func truncRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// csvErrMsg 把 encoding/csv 的错误转成运营看得懂的话。
func csvErrMsg(err error) string {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		switch {
		case errors.Is(pe.Err, csv.ErrQuote):
			return "引号没有成对出现（单元格里有双引号时要写成两个双引号）"
		case errors.Is(pe.Err, csv.ErrBareQuote):
			return "单元格中间出现了未转义的双引号"
		case errors.Is(pe.Err, csv.ErrFieldCount):
			return "列数与表头不一致"
		}
		return pe.Err.Error()
	}
	return err.Error()
}
