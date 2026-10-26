-- +goose Up
-- 后台权限可配置（docs/adr/0006）：菜单 → 权限点 → 角色 → 管理员。

-- 后台菜单：两级树，分组在顶层，页面与外链挂在分组下。
-- 页面由代码提供（path 即前端路由），只能改标题、图标、排序、显示与所属分组；分组与外链可在后台增删。
CREATE TABLE admin_menu (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    parent_id  bigint      REFERENCES admin_menu (id) ON DELETE RESTRICT,
    kind       text        NOT NULL CHECK (kind IN ('group', 'page', 'link')),
    code       text        NOT NULL UNIQUE CHECK (code ~ '^[a-z][a-z0-9_]{1,47}$'),
    title      text        NOT NULL CHECK (length(title) BETWEEN 1 AND 32),
    path       text        NOT NULL DEFAULT '',
    icon       text        NOT NULL DEFAULT '',
    sort       int         NOT NULL DEFAULT 0,
    visible    boolean     NOT NULL DEFAULT true,
    builtin    boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((kind = 'group') = (parent_id IS NULL)),
    CHECK (kind = 'group' OR path <> '')
);
CREATE INDEX admin_menu_parent_idx ON admin_menu (parent_id, sort, id);

-- 权限点目录：后端按权限点判定，后台只能把权限点授予角色，不能新建。sensitive 的权限点 S2-6 起需二次验证。
CREATE TABLE admin_permission (
    code      text    PRIMARY KEY CHECK (code ~ '^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$'),
    menu_id   bigint  NOT NULL REFERENCES admin_menu (id) ON DELETE CASCADE,
    name      text    NOT NULL,
    sensitive boolean NOT NULL DEFAULT false,
    sort      int     NOT NULL DEFAULT 0
);
CREATE INDEX admin_permission_menu_idx ON admin_permission (menu_id, sort);

-- 角色。超级角色（is_super）拥有全部权限（含以后新增的），不能改权限、停用或删除；全库只有一个。
CREATE TABLE admin_role (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code        text        NOT NULL UNIQUE CHECK (code ~ '^[a-z][a-z0-9_]{1,31}$'),
    name        text        NOT NULL CHECK (length(name) BETWEEN 1 AND 32),
    description text        NOT NULL DEFAULT '' CHECK (length(description) <= 200),
    is_super    boolean     NOT NULL DEFAULT false,
    builtin     boolean     NOT NULL DEFAULT false,
    status      text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    sort        int         NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (NOT is_super OR status = 'active')
);
CREATE UNIQUE INDEX admin_role_single_super_idx ON admin_role (is_super) WHERE is_super;

CREATE TABLE admin_role_permission (
    role_id         bigint NOT NULL REFERENCES admin_role (id) ON DELETE CASCADE,
    permission_code text   NOT NULL REFERENCES admin_permission (code) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_code)
);

-- ---------- 默认菜单（与 admin/src/config/navigation.ts 的页面登记一致） ----------
INSERT INTO admin_menu (kind, code, title, sort, builtin) VALUES
    ('group', 'overview',   '概览', 10, true),
    ('group', 'content',    '内容', 20, true),
    ('group', 'framework',  '框架', 30, true),
    ('group', 'whitelabel', '白标', 40, true),
    ('group', 'operations', '运营', 50, true),
    ('group', 'community',  '社区', 60, true),
    ('group', 'system',     '系统', 70, true),
    ('group', 'access',     '权限', 80, true);

INSERT INTO admin_menu (parent_id, kind, code, title, path, icon, sort, builtin)
SELECT g.id, 'page', p.code, p.title, p.path, p.icon, p.sort, true
FROM (VALUES
    ('overview',   'dashboard',         '数据看板',   '/',                  'LayoutDashboard',      10),
    ('overview',   'analytics_dramas',  '剧集分析',       '/analytics/dramas',  'BarChart3',            20),
    ('content',    'dramas',            '剧集',           '/dramas',            'Clapperboard',         10),
    ('content',    'actors',            '演员',           '/actors',            'Users',                20),
    ('content',    'tags',              '标签',           '/tags',              'Tags',                 30),
    ('content',    'import',            '批量导入',       '/import',            'Upload',               40),
    ('content',    'resource_checks',   '资源体检',       '/resource-checks',   'Activity',             50),
    ('content',    'resource_warm',     '资源预热',       '/resource-warm',     'Flame',                60),
    ('framework',  'taxonomies',        '筛选维度',       '/taxonomies',        'Filter',               10),
    ('framework',  'menus',             '前台菜单',       '/menus',             'ListTree',             20),
    ('framework',  'pages',             '静态页',         '/pages',             'FileText',             30),
    ('framework',  'topics',            '专题',           '/topics',            'BookOpen',             40),
    ('framework',  'blocks',            '页面区块',       '/blocks',            'LayoutTemplate',       50),
    ('whitelabel', 'brand',             '品牌与主题',     '/brand',             'Palette',              10),
    ('whitelabel', 'seo',               'SEO 与统计',     '/seo',               'Search',               20),
    ('operations', 'ads',               '广告位',         '/ads',               'Megaphone',            10),
    ('operations', 'announcements',     '公告',           '/announcements',     'Flag',                 20),
    ('operations', 'articles',          '资讯',           '/articles',          'Newspaper',            30),
    ('operations', 'ranks',             '排行干预',       '/ranks',             'Trophy',               40),
    ('operations', 'hotsearch',         '热搜词',         '/hotsearch',         'Zap',                  50),
    ('operations', 'finance',           '收入与成本',     '/analytics/finance', 'Wallet',               60),
    ('community',  'moderation',        '评论与弹幕审核', '/moderation',        'MessageSquareWarning', 10),
    ('community',  'reports',           '举报队列',       '/reports',           'Flag',                 20),
    ('community',  'users',             '用户',           '/users',             'UserCog',              30),
    ('community',  'blockwords',        '屏蔽词',         '/blockwords',        'Ban',                  40),
    ('system',     'storage_providers', '存储源',         '/storage/providers', 'Cloud',                10),
    ('system',     'storage_lines',     '线路',           '/storage/lines',     'Network',              20),
    ('system',     'storage_preview',   '选线预览与健康', '/storage/preview',   'Radar',                30),
    ('system',     'storage_settings',  '存储设置',       '/storage/settings',  'SlidersHorizontal',    40),
    ('system',     'ftp',               '资源机 FTP',     '/ftp',               'HardDrive',            50),
    ('system',     'cache',             '缓存',           '/cache',             'Gauge',                60),
    ('system',     'config',            '配置导出导入',   '/config',            'FileCode',             70),
    ('system',     'apikeys',           'API Key',        '/apikeys',           'KeyRound',             80),
    ('system',     'audit',             '审计日志',       '/audit',             'Database',             90),
    ('system',     'trash',             '回收站',         '/trash',             'Trash2',              100),
    ('system',     'features',          '功能开关与年龄门', '/features',        'Settings2',           110),
    ('system',     'domains',           '域名与证书',     '/domains',           'Globe',               120),
    ('system',     'security',          '安全中心',       '/security',          'Cable',               130),
    ('access',     'admins',            '管理员',         '/admins',            'ShieldCheck',          10),
    ('access',     'roles',             '角色',           '/roles',             'UserRoundCog',         20),
    ('access',     'role_permissions',  '权限矩阵',       '/role-permissions',  'Grid3x3',              30),
    ('access',     'admin_menus',       '后台菜单',       '/admin-menus',       'PanelLeft',            40)
) AS p (group_code, code, title, path, icon, sort)
JOIN admin_menu g ON g.code = p.group_code;

-- ---------- 权限点：每个页面「查看」，有写操作的页面「修改」，另有三个单独授权的操作 ----------
INSERT INTO admin_permission (code, menu_id, name, sensitive, sort)
SELECT code || '.view', id, '查看', false, 0 FROM admin_menu WHERE kind = 'page';

-- 改线路、导入配置、生成 API Key、改管理员等为高敏操作（docs/03 第 2.3 节、docs/07 第 2 节 🔐）
INSERT INTO admin_permission (code, menu_id, name, sensitive, sort)
SELECT code || '.edit', id, '修改',
       code IN ('storage_providers', 'storage_lines', 'ftp', 'config', 'apikeys', 'domains', 'admins', 'roles', 'role_permissions'),
       1
FROM admin_menu
WHERE kind = 'page' AND code NOT IN ('analytics_dramas', 'storage_preview', 'audit', 'security');

INSERT INTO admin_permission (code, menu_id, name, sensitive, sort)
SELECT v.code, m.id, v.name, v.sensitive, 2
FROM (VALUES
    ('blocks.custom_html', 'blocks', '自定义 HTML', false),
    ('ads.custom_html',    'ads',    '自定义 HTML', false),
    ('seo.analytics_code', 'seo',    '统计代码',    true)
) AS v (code, page, name, sensitive)
JOIN admin_menu m ON m.code = v.page;

-- ---------- 内置角色与默认授权（与原 docs/03 第 2.5 节矩阵一致：四种角色都能查看全部页面） ----------
INSERT INTO admin_role (code, name, description, is_super, builtin, sort) VALUES
    ('superadmin', '超级管理员', '拥有全部权限，含以后新增的权限点', true,  true, 0),
    ('operator',   '运营',       '内容、框架、白标、运营、社区可改；系统与权限只读', false, true, 10),
    ('moderator',  '审核员',     '社区可改，其余只读', false, true, 20),
    ('readonly',   '只读',       '全部只读', false, true, 30);

INSERT INTO admin_role_permission (role_id, permission_code)
SELECT r.id, p.code
FROM admin_role r CROSS JOIN admin_permission p
WHERE r.code IN ('operator', 'moderator', 'readonly') AND p.code LIKE '%.view';

INSERT INTO admin_role_permission (role_id, permission_code)
SELECT r.id, p.code
FROM admin_role r
JOIN admin_permission p ON p.code LIKE '%.edit'
JOIN admin_menu page ON page.id = p.menu_id
JOIN admin_menu grp ON grp.id = page.parent_id
WHERE (r.code = 'operator' AND grp.code IN ('overview', 'content', 'framework', 'whitelabel', 'operations', 'community'))
   OR (r.code = 'moderator' AND grp.code = 'community');

-- ---------- 管理员改为引用角色表；session_version 递增即吊销该管理员的全部会话 ----------
ALTER TABLE admin_user ADD COLUMN role_id bigint REFERENCES admin_role (id);
UPDATE admin_user u SET role_id = r.id FROM admin_role r WHERE r.code = u.role;
ALTER TABLE admin_user ALTER COLUMN role_id SET NOT NULL;
ALTER TABLE admin_user DROP COLUMN role;
ALTER TABLE admin_user ADD COLUMN session_version int NOT NULL DEFAULT 0;
CREATE INDEX admin_user_role_idx ON admin_user (role_id);

-- +goose Down
ALTER TABLE admin_user ADD COLUMN role text;
-- 自定义角色无法还原为旧的四种角色，按最小权限降为 readonly
UPDATE admin_user u
SET role = CASE WHEN r.code IN ('superadmin', 'operator', 'moderator', 'readonly') THEN r.code ELSE 'readonly' END
FROM admin_role r
WHERE r.id = u.role_id;
ALTER TABLE admin_user ALTER COLUMN role SET NOT NULL;
ALTER TABLE admin_user ADD CONSTRAINT admin_user_role_check CHECK (role IN ('superadmin', 'operator', 'moderator', 'readonly'));
DROP INDEX admin_user_role_idx;
ALTER TABLE admin_user DROP COLUMN role_id;
ALTER TABLE admin_user DROP COLUMN session_version;
DROP TABLE admin_role_permission;
DROP TABLE admin_role;
DROP TABLE admin_permission;
DROP TABLE admin_menu;
