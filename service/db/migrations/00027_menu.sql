-- +goose Up
-- 前台菜单（docs/02 第 3.2 节、docs/06 第 12 节，S3-2 / A3-2，docs/specs/A3-1.md）：顶部导航、手机底栏、手机侧边抽屉、页脚。
-- 随 GET /api/v1/site-config 的 menus 段下发：下发全部启用项并带上 visible_to 与 device，由前台按登录态与设备判断显示
-- （站点配置是共享缓存的公共数据，不能按身份裁剪）。链接目标失效（静态页删除或改回草稿、频道取值停用等）时下发层过滤。
-- 设置类数据，删除不进回收站。
CREATE TABLE menu (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    position    text        NOT NULL CHECK (position IN ('top', 'mobile_tabbar', 'mobile_drawer', 'footer')),
    label       text        NOT NULL CHECK (length(label) BETWEEN 1 AND 20),
    -- internal 站内页白名单（home/shua/explore/rank/topic/search/me/way_home）；page/channel/topic/tag 为对应记录的 slug；
    -- external 为 http(s) 完整地址
    link_type   text        NOT NULL CHECK (link_type IN ('internal', 'page', 'channel', 'topic', 'tag', 'external')),
    link_value  text        NOT NULL CHECK (link_value <> ''),
    group_title text        NOT NULL DEFAULT '',  -- 页脚分组标题，只有 footer 用
    icon        text        NOT NULL DEFAULT '',  -- lucide 图标名
    tabbar_icon text        NOT NULL DEFAULT '',  -- 手机底栏图标，只有 mobile_tabbar 用
    badge       text        NOT NULL DEFAULT '',
    visible_to  text        NOT NULL DEFAULT 'all' CHECK (visible_to IN ('all', 'auth', 'guest')),
    device      text        NOT NULL DEFAULT 'all' CHECK (device IN ('all', 'mobile', 'desktop')),
    new_tab     boolean     NOT NULL DEFAULT false,
    status      text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    sort        integer     NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
-- 后台按位置分组显示、前台下发启用项，都按 (position, sort) 取
CREATE INDEX menu_position_sort_idx ON menu (position, sort, id);

-- +goose Down
DROP TABLE menu;
