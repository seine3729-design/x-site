package pg

import "io/fs"

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		// 目录名在编译期固定，出错说明嵌入配置有误，属于程序错误。
		panic(err)
	}
	return sub
}
