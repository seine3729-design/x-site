-- +goose Up
-- 全库表结构注释（docs/02 数据模型设计）。
--
-- 为什么单独发一个迁移、而不是回去改历史迁移文件：历史迁移已经在各环境执行过，
-- 改文件不会重跑，数据库里仍然没有注释。只有把 COMMENT ON 作为新迁移执行，
-- 注释才会真正进入 pg_description，psql \d+、DBeaver / Navicat / pgAdmin 以及
-- sqlc 之外的任何工具都能直接看到字段含义。
--
-- 覆盖范围：public schema 下全部业务表（83 张，含分区父表 event_raw）。
-- 不覆盖 goose_db_version（goose 自己的版本表）与 river_*（River 队列自建表）：
-- 它们由第三方工具维护，注释应由其自身版本管理。
-- event_raw 的日分区（event_raw_YYYYMMDD）由周期任务动态创建，PG 的 COMMENT 不会
-- 从父表传播到分区，这里只注释父表 —— 父表才是模型定义处。
--
-- 注释内容与 docs/02 以及各建表迁移里的行内说明一致；两者冲突时以建表迁移的实际约束为准。

-- ======================================================================
-- 1. 站点配置
-- ======================================================================

COMMENT ON TABLE site_config IS '站点配置：单行多列 jsonb，对应后台各配置段；version 每次前台可见的变更 +1，供 ETag 判断（docs/02 第 3.6 节）';
COMMENT ON COLUMN site_config.id IS '固定为 1：CHECK 约束保证全表只有一行';
COMMENT ON COLUMN site_config.version IS '站点配置版本号，前台与 APP 按它判断配置是否变化；只有下发给前台的段变更时 +1';
COMMENT ON COLUMN site_config.site IS '白标基础段：site_name / slogan / copyright / support_url / logo_light / favicon';
COMMENT ON COLUMN site_config.theme IS '主题段：preset / color_scheme / allow_scheme_toggle / colors / radius / cover_ratio / font';
COMMENT ON COLUMN site_config.layout IS '布局段：header_style / card_style / play_mode / mobile_tabbar';
COMMENT ON COLUMN site_config.seo IS 'SEO 段：按页面的 title_templates / desc_templates，以及 og_image / robots / sitemap / structured_data / verifications';
COMMENT ON COLUMN site_config.analytics IS '统计代码段：head_snippet / body_snippet。只有后台接口读取，不对前台下发';
COMMENT ON COLUMN site_config.age_gate IS '年龄门段：enabled / title / desc / deny_text / confirm_text / deny_url / remember_days';
COMMENT ON COLUMN site_config.features IS '功能开关段：comments / danmaku / register / guest_interaction / ranks / share_poster / pwa / search_suggest';
COMMENT ON COLUMN site_config.addresses IS '地址段：notice / socials / telegram / latest_url / backup_urls / publish_page / permanent_url';
COMMENT ON COLUMN site_config.ranking IS '榜单计分系数：play / follow / like / finish（各 0–1000，默认 1/3/2/2）。只给后台与重新排名任务读，不下发前台，修改不改 version';
COMMENT ON COLUMN site_config.comment_policy IS '评论策略：pre_moderation=true 时所有评论先进待审。后端专用段，不下发前台、不改 version';
COMMENT ON COLUMN site_config.updated_at IS '最近一次保存时间';

-- ======================================================================
-- 2. 后台身份、权限与审计
-- ======================================================================

COMMENT ON TABLE admin_user IS '运营后台管理员（docs/02 第 6.1 节）。被审计引用后不能物理删除，只能停用；会话存 Valkey';
COMMENT ON COLUMN admin_user.id IS '管理员 ID';
COMMENT ON COLUMN admin_user.username IS '登录名：统一存小写，3–32 位，大小写不敏感唯一';
COMMENT ON COLUMN admin_user.password_hash IS '密码哈希（Argon2id 编码串）。人选的口令必须用慢哈希';
COMMENT ON COLUMN admin_user.status IS '账号状态：active 正常 / inactive 停用 / unused 新建后未登录过（可登录，首次登录后转 active）';
COMMENT ON COLUMN admin_user.totp_secret_enc IS 'TOTP 密钥密文（信封加密，KEK 来自 ADMIN_TOTP_KEK_SECRET）；未开启二次验证时为空';
COMMENT ON COLUMN admin_user.mfa_required IS '是否强制要求二次验证；超级管理员由业务层强制开启';
COMMENT ON COLUMN admin_user.last_login_at IS '最近一次登录成功时间';
COMMENT ON COLUMN admin_user.role_id IS '所属角色（admin_role）';
COMMENT ON COLUMN admin_user.session_version IS '会话版本：重置密码、强制下线时 +1，旧会话立即作废';
COMMENT ON COLUMN admin_user.created_at IS '创建时间';
COMMENT ON COLUMN admin_user.updated_at IS '最近修改时间';

COMMENT ON TABLE admin_login_log IS '后台登录记录，安全中心展示（docs/06 第 39 节）。只存 IP 网段与 UA 摘要，保留 180 天';
COMMENT ON COLUMN admin_login_log.id IS '记录 ID';
COMMENT ON COLUMN admin_login_log.admin_id IS '命中的管理员；用户名不存在时为空';
COMMENT ON COLUMN admin_login_log.username_tried IS '本次尝试使用的登录名（原样记录，用于识别撞库）';
COMMENT ON COLUMN admin_login_log.result IS '结果：success 成功 / bad_password 密码错 / inactive 账号停用 / mfa_failed 二次验证失败 / locked 已锁定';
COMMENT ON COLUMN admin_login_log.ip_prefix IS '来源 IP 网段（/16），不存完整 IP';
COMMENT ON COLUMN admin_login_log.ua_digest IS 'User-Agent 摘要，不存原始 UA';
COMMENT ON COLUMN admin_login_log.created_at IS '发生时间';

COMMENT ON TABLE admin_menu IS '后台菜单：一级 / 二级两层树（docs/adr/0006）。页面由代码提供，内置项不能删除、路径不能改';
COMMENT ON COLUMN admin_menu.id IS '菜单 ID';
COMMENT ON COLUMN admin_menu.parent_id IS '上级菜单；一级菜单为空。目录只能是一级（CHECK 约束），上级必须是目录由业务层校验';
COMMENT ON COLUMN admin_menu.kind IS '类型：group 目录 / page 页面 / link 外链';
COMMENT ON COLUMN admin_menu.code IS '菜单编码，全局唯一；权限点按 <code>.<动作> 命名';
COMMENT ON COLUMN admin_menu.title IS '显示标题，1–32 字';
COMMENT ON COLUMN admin_menu.path IS '页面为前端路由、外链为 http(s) 地址、目录为空';
COMMENT ON COLUMN admin_menu.icon IS 'lucide 图标名';
COMMENT ON COLUMN admin_menu.sort IS '同级排序，数字小的在前';
COMMENT ON COLUMN admin_menu.visible IS '是否在侧栏显示；隐藏不影响权限判定';
COMMENT ON COLUMN admin_menu.builtin IS '是否内置：内置菜单不能删除，页面路径不能改';
COMMENT ON COLUMN admin_menu.created_at IS '创建时间';
COMMENT ON COLUMN admin_menu.updated_at IS '最近修改时间';

COMMENT ON TABLE admin_permission IS '权限点目录：后端按权限点判定。由迁移定义，后台只能授予角色、不能新建';
COMMENT ON COLUMN admin_permission.code IS '权限点编码 <页面>.<动作>，如 dramas.edit';
COMMENT ON COLUMN admin_permission.menu_id IS '所属页面（admin_menu）；页面删除时级联删除';
COMMENT ON COLUMN admin_permission.name IS '动作名称，如「查看」「修改」';
COMMENT ON COLUMN admin_permission.sensitive IS '是否高敏操作：命中时需要重新做二次验证（S2-6 起）';
COMMENT ON COLUMN admin_permission.sort IS '页面内的展示顺序';

COMMENT ON TABLE admin_role IS '后台角色。超级角色（is_super）拥有全部权限（含以后新增的），全库唯一，不能改权限、停用或删除';
COMMENT ON COLUMN admin_role.id IS '角色 ID';
COMMENT ON COLUMN admin_role.code IS '角色编码，创建后不能改';
COMMENT ON COLUMN admin_role.name IS '角色名称，1–32 字';
COMMENT ON COLUMN admin_role.description IS '角色说明，不超过 200 字';
COMMENT ON COLUMN admin_role.is_super IS '是否超级角色：自动拥有全部权限点，不存授权行，全库只能有一个';
COMMENT ON COLUMN admin_role.builtin IS '是否内置：只有超级管理员为 true';
COMMENT ON COLUMN admin_role.status IS '状态：active 启用 / inactive 停用（停用后成员不能登录）';
COMMENT ON COLUMN admin_role.sort IS '列表排序';
COMMENT ON COLUMN admin_role.created_at IS '创建时间';
COMMENT ON COLUMN admin_role.updated_at IS '最近修改时间';

COMMENT ON TABLE admin_role_permission IS '角色授权：角色与权限点的多对多关系。超级角色不存授权行';
COMMENT ON COLUMN admin_role_permission.role_id IS '角色（admin_role）';
COMMENT ON COLUMN admin_role_permission.permission_code IS '权限点（admin_permission）';

COMMENT ON TABLE admin_passkey IS '后台二次验证 Passkey / WebAuthn 凭据（S2-6）。一个管理员可注册多把';
COMMENT ON COLUMN admin_passkey.id IS '凭据记录 ID';
COMMENT ON COLUMN admin_passkey.admin_id IS '所属管理员；管理员删除时级联删除';
COMMENT ON COLUMN admin_passkey.credential_id IS 'WebAuthn 凭据 ID（原始字节），全局唯一，断言时按它查凭据';
COMMENT ON COLUMN admin_passkey.public_key IS 'COSE 格式公钥，用于校验断言签名';
COMMENT ON COLUMN admin_passkey.sign_count IS '签名计数器，防重放：每次断言校验后写回，计数回退即疑似克隆';
COMMENT ON COLUMN admin_passkey.transports IS '可用传输方式（usb / nfc / ble / internal 等），供前端优化选择器';
COMMENT ON COLUMN admin_passkey.name IS '用户给这把钥匙起的名字，便于在列表里区分与删除';
COMMENT ON COLUMN admin_passkey.created_at IS '注册时间';
COMMENT ON COLUMN admin_passkey.last_used_at IS '最近一次成功断言时间';

COMMENT ON TABLE audit_log IS '审计日志（docs/02 第 6.2 节）。只允许 INSERT，哈希链保证不可篡改；密码与密钥字段写入前脱敏';
COMMENT ON COLUMN audit_log.id IS '记录 ID，决定物理顺序；校验哈希链时按它升序走链';
COMMENT ON COLUMN audit_log.admin_id IS '操作人（admin_user）。外键无级联：被引用的管理员不能物理删除';
COMMENT ON COLUMN audit_log.action IS '动作标识，如 drama.update';
COMMENT ON COLUMN audit_log.target_type IS '目标实体类型（约 35 种）';
COMMENT ON COLUMN audit_log.target_id IS '目标主键，用 text 以兼容订单号等非数字编号';
COMMENT ON COLUMN audit_log.diff_before IS '变更前内容（脱敏后）';
COMMENT ON COLUMN audit_log.diff_after IS '变更后内容（脱敏后）';
COMMENT ON COLUMN audit_log.ip_prefix IS '操作来源 IP 网段，不存完整 IP';
COMMENT ON COLUMN audit_log.request_id IS '请求 ID，与服务端日志串联';
COMMENT ON COLUMN audit_log.prev_hash IS '上一条记录的 hash；启用哈希链之前的旧记录为空串';
COMMENT ON COLUMN audit_log.hash IS 'SHA256(prev_hash ‖ 各字段)，字段间以 0x1F 分隔；刻意不含 id（插入时才生成，会成循环依赖）';
COMMENT ON COLUMN audit_log.created_at IS '发生时间';

COMMENT ON TABLE config_backup IS '配置备份（S4-4）：导出的备份正文留一份在库里，导入前自动存档以便回滚';
COMMENT ON COLUMN config_backup.id IS '备份 ID';
COMMENT ON COLUMN config_backup.format IS '备份格式版本号';
COMMENT ON COLUMN config_backup.reason IS '产生原因：manual 手动导出 / before_import 导入前自动存档';
COMMENT ON COLUMN config_backup.admin_id IS '操作人。不加外键：备份要比管理员账号活得久，账号清理后仍需可追溯';
COMMENT ON COLUMN config_backup.note IS '运营填写的备注';
COMMENT ON COLUMN config_backup.payload IS '备份正文，与导出文件的 data 段一致';
COMMENT ON COLUMN config_backup.signature IS '对「格式版本 + 正文」的 HMAC，导入时校验完整性';
COMMENT ON COLUMN config_backup.created_at IS '备份时间';

COMMENT ON TABLE api_key IS '开放平台 API Key（A4-3）：供采集程序等服务端调用方对接。完整密钥仅生成时返回一次';
COMMENT ON COLUMN api_key.id IS '密钥记录 ID';
COMMENT ON COLUMN api_key.name IS '用途说明（做什么用的），1–60 字';
COMMENT ON COLUMN api_key.owner IS '使用方（给谁用的）';
COMMENT ON COLUMN api_key.key_hash IS '完整密钥的 SHA-256。密钥是 32 字节随机串、熵足够高，不需要慢哈希（每个请求都要校验）';
COMMENT ON COLUMN api_key.key_hint IS '密钥开头几位，供后台识别与搜索（如 xsk_a1b2c3）；之后任何地方都取不回完整密钥';
COMMENT ON COLUMN api_key.scopes IS '权限范围：drama.push 推送剧集入库 / analytics.read 只读运营数据';
COMMENT ON COLUMN api_key.status IS '状态：active 启用 / inactive 停用';
COMMENT ON COLUMN api_key.used_count IS '累计调用次数';
COMMENT ON COLUMN api_key.last_used_at IS '最近调用时间；为空时后台显示「未用过」';
COMMENT ON COLUMN api_key.created_by IS '签发人。不加外键：密钥记录要比管理员账号活得久';
COMMENT ON COLUMN api_key.created_at IS '创建时间';
COMMENT ON COLUMN api_key.updated_at IS '最近修改时间';

-- ======================================================================
-- 3. 内容域：剧集、分集、标签、筛选维度、专题
-- ======================================================================

COMMENT ON TABLE drama IS '剧集（docs/02 第 2.1 节）。状态机 draft ──上架──► published ──下架──► offline；published 且 publish_at > now() 为定时未到点，前台不可见。删除进回收站（deleted_at），30 天后物理删除';
COMMENT ON COLUMN drama.id IS '剧编号 #ID，全站引用';
COMMENT ON COLUMN drama.title IS '剧名；建有 pg_bigm 索引供中文模糊搜索';
COMMENT ON COLUMN drama.aliases IS '别名数组，用于搜索与导入去重；索引建在 aliases_text(aliases) 上';
COMMENT ON COLUMN drama.slug IS '网址别名，可空（前台按 /d/:id 访问）；填写时全站唯一';
COMMENT ON COLUMN drama.summary IS '剧情简介';
COMMENT ON COLUMN drama.cover_portrait IS '竖封面相对 key（后台上传生成的 img/...）';
COMMENT ON COLUMN drama.cover_landscape IS '横封面相对 key（后台上传生成的 img/...）';
COMMENT ON COLUMN drama.badge IS '角标文案，如 HOT；空串为不显示';
COMMENT ON COLUMN drama.score IS '评分（运营手填，如 9.9）；NULL 表示未填';
COMMENT ON COLUMN drama.year IS '年份（1900–2100）；NULL 表示未填';
COMMENT ON COLUMN drama.status IS '状态：draft 草稿 / published 已上架 / offline 已下架';
COMMENT ON COLUMN drama.publish_at IS '定时上架时间；NULL 表示未设置，晚于当前时间时前台不可见';
COMMENT ON COLUMN drama.is_finished IS '是否完结';
COMMENT ON COLUMN drama.sort_weight IS '「推荐」排序权重，越大越靠前';
COMMENT ON COLUMN drama.view_base IS '初始播放量（运营填的虚量），热度排序时与真实播放量相加';
COMMENT ON COLUMN drama.view_count IS '真实播放量；先在 Valkey 累加，worker 周期回写';
COMMENT ON COLUMN drama.hot_score IS '冗余列：热度分，定时任务按 view_base + view_count 刷新，供 (status, hot_score desc, id desc) 游标分页';
COMMENT ON COLUMN drama.latest_episode IS '冗余列：前台可见的最大集号（「更新至 N 集」），只算 media_status=ready 的分集';
COMMENT ON COLUMN drama.seo_title IS 'SEO 标题；空串时按站点模板生成';
COMMENT ON COLUMN drama.seo_desc IS 'SEO 描述；空串时按站点模板生成';
COMMENT ON COLUMN drama.source IS '来源标识（采集批次），与 import_log.source 对应';
COMMENT ON COLUMN drama.created_at IS '创建时间';
COMMENT ON COLUMN drama.updated_at IS '最近修改时间';
COMMENT ON COLUMN drama.deleted_at IS '软删除时间：非空表示在回收站，30 天后物理清理';

COMMENT ON TABLE episode IS '分集（docs/02 第 2.2 节）。(drama_id, number) 唯一；直接删除不进回收站，剧物理删除时级联删除';
COMMENT ON COLUMN episode.id IS '分集 ID';
COMMENT ON COLUMN episode.drama_id IS '所属剧；剧删除时级联删除';
COMMENT ON COLUMN episode.number IS '集号（第几集），大于 0';
COMMENT ON COLUMN episode.title IS '本集标题，可留空';
COMMENT ON COLUMN episode.video_object_id IS '当前使用的视频对象（media_object），转封装完成时写入；被引用的媒体对象不能删除';
COMMENT ON COLUMN episode.cover IS '本集封面相对 key（后台上传生成的 img/...）';
COMMENT ON COLUMN episode.poster IS '首帧图（WebP，图片存储中的相对 key），转封装生成，播放器出画面前显示';
COMMENT ON COLUMN episode.duration_sec IS '时长（秒）；为 0 时由转封装的探测结果回填，已填写的不覆盖';
COMMENT ON COLUMN episode.access IS '观看门槛：free 免费 / login 登录可看 / vip 会员可看（非会员按 trial_sec 试看）';
COMMENT ON COLUMN episode.trial_sec IS '非会员可试看秒数，0 表示不试看';
COMMENT ON COLUMN episode.media_status IS '媒体状态：pending 没有视频 / transcoding 首次转封装中 / ready 可播放 / failed 转封装失败。已 ready 的分集重新转封装时保持 ready';
COMMENT ON COLUMN episode.renditions IS '当前产物的码率阶梯 [{name,width,height,bitrate,peak_bitrate,codecs}]，从低到高；码率为 bps，codecs 为 RFC 6381 编码';
COMMENT ON COLUMN episode.key_ver IS '当前产物使用的 HLS 内容密钥版本（密钥本身在 media_key）；0 表示尚未生成';
COMMENT ON COLUMN episode.source_hash IS '当前产物的源文件 SHA-256，转封装任务的幂等键';
COMMENT ON COLUMN episode.created_at IS '创建时间';
COMMENT ON COLUMN episode.updated_at IS '最近修改时间';

COMMENT ON TABLE tag IS '标签（docs/02 第 2.3 节）。合并是软合并：被并入的标签记 merged_into_id，前台按目标标签展示';
COMMENT ON COLUMN tag.id IS '标签 ID';
COMMENT ON COLUMN tag.name IS '标签名';
COMMENT ON COLUMN tag.slug IS '网址别名，全站唯一（标签页 /t/:slug）';
COMMENT ON COLUMN tag.is_hidden IS '敏感标签：过年龄门后才显示，且不被搜索引擎收录';
COMMENT ON COLUMN tag.status IS '状态：active 正常 / hidden 不在前台显示';
COMMENT ON COLUMN tag.merged_into_id IS '软合并目标标签；非空表示本标签已被并入该标签';
COMMENT ON COLUMN tag.created_at IS '创建时间';

COMMENT ON TABLE drama_tag IS '剧集与标签的多对多关系。反向索引 (tag_id, drama_id) 供按标签查剧';
COMMENT ON COLUMN drama_tag.drama_id IS '剧集（drama）';
COMMENT ON COLUMN drama_tag.tag_id IS '标签（tag）';

COMMENT ON TABLE taxonomy IS '筛选维度（docs/02 第 2.4 节）：探索页的一组筛选条，如题材、地区、年份';
COMMENT ON COLUMN taxonomy.id IS '维度 ID';
COMMENT ON COLUMN taxonomy.name IS '维度名称';
COMMENT ON COLUMN taxonomy.slug IS '维度别名，全站唯一；探索页按「维度 slug + 取值 slug」筛选';
COMMENT ON COLUMN taxonomy.multi_select IS '是否可多选：多选维度追加取值，单选维度由业务层替换';
COMMENT ON COLUMN taxonomy.show_in_explore IS '是否在探索页显示筛选条';
COMMENT ON COLUMN taxonomy.status IS '状态：active 启用 / inactive 停用';
COMMENT ON COLUMN taxonomy.sort IS '筛选条排序';

COMMENT ON TABLE taxonomy_term IS '维度取值（docs/02 第 2.4 节）。删除维度时取值级联删除 —— 取值被删会同时清空全站剧集的该维度归属';
COMMENT ON COLUMN taxonomy_term.id IS '取值 ID';
COMMENT ON COLUMN taxonomy_term.taxonomy_id IS '所属维度；维度删除时级联删除';
COMMENT ON COLUMN taxonomy_term.name IS '取值名称';
COMMENT ON COLUMN taxonomy_term.slug IS '取值别名，在维度内唯一；设为频道的取值 slug 全站唯一（频道页 /c/:slug）';
COMMENT ON COLUMN taxonomy_term.status IS '状态：active 启用 / inactive 停用（前台筛选条不显示、频道页 404）';
COMMENT ON COLUMN taxonomy_term.is_channel IS '是否有独立频道页；为真时 slug 需全站唯一';
COMMENT ON COLUMN taxonomy_term.code IS '取值编号，供区块与专题的自动规则引用；填写时全站唯一';
COMMENT ON COLUMN taxonomy_term.sort IS '取值排序';

COMMENT ON TABLE drama_term IS '剧集与维度取值的多对多关系。单选维度每部剧只能有一个取值，由业务层保存时替换';
COMMENT ON COLUMN drama_term.drama_id IS '剧集（drama）';
COMMENT ON COLUMN drama_term.term_id IS '维度取值（taxonomy_term）';

COMMENT ON TABLE topic IS '专题（docs/02 第 2.5 节）：手选或按规则自动选出的一组剧';
COMMENT ON COLUMN topic.id IS '专题 ID';
COMMENT ON COLUMN topic.title IS '专题标题';
COMMENT ON COLUMN topic.slug IS '网址别名，全站唯一（专题页 /topic/:slug）';
COMMENT ON COLUMN topic.summary IS '专题简介';
COMMENT ON COLUMN topic.status IS '状态：draft 草稿 / published 已发布';
COMMENT ON COLUMN topic.source_type IS '选剧方式：manual 手选（topic_drama）/ auto 按 rule_json 规则';
COMMENT ON COLUMN topic.rule_json IS '自动规则：{term_codes[], tag_id, sort(latest|hot|recommend), window(all|7d|14d|30d|90d), limit(1–60)}，同时满足全部取值与标签；{} 按默认规则解释';
COMMENT ON COLUMN topic.faq_json IS '问答对 [{question, answer}]，纯文本，用于专题页 SEO';
COMMENT ON COLUMN topic.list_in_index IS '是否列在专题列表页';
COMMENT ON COLUMN topic.created_at IS '创建时间';

COMMENT ON TABLE topic_drama IS '专题手选剧：保存时按专题整份替换';
COMMENT ON COLUMN topic_drama.topic_id IS '专题（topic）';
COMMENT ON COLUMN topic_drama.drama_id IS '剧集（drama）';
COMMENT ON COLUMN topic_drama.sort IS '专题内的展示顺序';

-- ======================================================================
-- 4. 站点编排域：区块、菜单、静态页、广告、公告、热搜、排行干预
-- （都是设置类数据，删除不进回收站，误删靠审计日志重建）
-- ======================================================================

COMMENT ON TABLE block IS '页面区块（docs/02 第 3.1 节）：首页与频道页由区块拼装，单页面最多 50 个。前台 GET /api/v1/home?page= 一次下发该页面全部启用区块及其数据';
COMMENT ON COLUMN block.id IS '区块 ID';
COMMENT ON COLUMN block.page IS '所属页面：home 或 channel:{频道取值 slug}';
COMMENT ON COLUMN block.type IS '区块类型：carousel 轮播 / feature 特写 / poster_grid 海报网格 / h_scroll 横滑 / rank 榜单 / topic_group 专题组 / tag_cloud 标签云 / ad 广告位 / announce 公告条 / custom_html 自定义';
COMMENT ON COLUMN block.title IS '区块标题';
COMMENT ON COLUMN block.subtitle IS '区块副标题';
COMMENT ON COLUMN block.item_limit IS '最多显示几项（1–60，默认 24）。limit 是 SQL 保留字，列名加了 item_ 前缀';
COMMENT ON COLUMN block.status IS '状态：active 启用 / inactive 停用';
COMMENT ON COLUMN block.more_link IS '「查看全部」链接';
COMMENT ON COLUMN block.cols_desktop IS '电脑端每行个数（2–8，默认 6）';
COMMENT ON COLUMN block.device IS '投放设备：all 全部 / mobile 手机 / desktop 电脑';
COMMENT ON COLUMN block.source_type IS '剧类区块的来源：manual 指定编号 / topic 用某专题 / auto 按规则选。非剧类区块恒为 auto';
COMMENT ON COLUMN block.source_json IS '来源与类型参数，按类型只存用得到的键：drama_ids / topic_id / {term_codes,tag_id,sort,window} / rank_board / ad_slot / announce_kind / topic_ids / tag_ids';
COMMENT ON COLUMN block.custom_html IS '自定义 HTML（白名单清洗后）；编辑需要 blocks.custom_html 权限';
COMMENT ON COLUMN block.sort IS '区块在该页面内的顺序，由后台拖拽调整';
COMMENT ON COLUMN block.created_at IS '创建时间';
COMMENT ON COLUMN block.updated_at IS '最近修改时间';

COMMENT ON TABLE menu IS '前台菜单（docs/02 第 3.2 节）。随 GET /api/v1/site-config 的 menus 段下发：只下发启用且链接目标当前可见的项；手机底栏最多 5 项。与后台菜单 admin_menu 是两张不同的表';
COMMENT ON COLUMN menu.id IS '菜单项 ID';
COMMENT ON COLUMN menu.position IS '位置：top 顶部导航 / mobile_tabbar 手机底栏 / mobile_drawer 手机侧边抽屉 / footer 页脚';
COMMENT ON COLUMN menu.label IS '菜单文案，1–20 字';
COMMENT ON COLUMN menu.link_type IS '链接类型：internal 站内页白名单 / page 静态页 / channel 频道 / topic 专题 / tag 标签 / external 外部地址';
COMMENT ON COLUMN menu.link_value IS '链接目标：internal 为站内页标识（home/shua/explore/rank/topic/search/me/way_home）；page/channel/topic/tag 为对应记录的 slug；external 为 http(s) 完整地址';
COMMENT ON COLUMN menu.group_title IS '页脚分组标题，只有 footer 位置使用';
COMMENT ON COLUMN menu.icon IS 'lucide 图标名';
COMMENT ON COLUMN menu.tabbar_icon IS '手机底栏图标，只有 mobile_tabbar 位置使用';
COMMENT ON COLUMN menu.badge IS '角标文案，如 NEW';
COMMENT ON COLUMN menu.visible_to IS '可见人群：all 全部 / auth 已登录 / guest 未登录。原样下发由前台判断（站点配置是共享缓存，不能按身份裁剪）';
COMMENT ON COLUMN menu.device IS '投放设备：all 全部 / mobile 手机 / desktop 电脑，原样下发由前台判断';
COMMENT ON COLUMN menu.new_tab IS '是否在新标签页打开';
COMMENT ON COLUMN menu.status IS '状态：active 启用 / inactive 停用';
COMMENT ON COLUMN menu.sort IS '同一位置内的顺序，由后台拖拽调整';
COMMENT ON COLUMN menu.created_at IS '创建时间';
COMMENT ON COLUMN menu.updated_at IS '最近修改时间';

COMMENT ON TABLE page IS '静态页（docs/02 第 3.3 节）：条款、隐私、FAQ 等，前台 /p/:slug。删除或改回草稿后，指向它的前台菜单项自动隐藏';
COMMENT ON COLUMN page.id IS '静态页 ID';
COMMENT ON COLUMN page.title IS '页面标题，1–100 字';
COMMENT ON COLUMN page.slug IS '网址别名，全站唯一（前台 /p/:slug）';
COMMENT ON COLUMN page.body IS '正文：按白名单清洗后的 HTML（两种格式都清洗，清洗后为空则拒绝保存）';
COMMENT ON COLUMN page.body_format IS '编辑方式：rich 富文本编辑器产出 / html 直接写 HTML。入库都是清洗后的结果';
COMMENT ON COLUMN page.status IS '状态：draft 草稿（不对前台下发）/ published 已发布';
COMMENT ON COLUMN page.seo_title IS 'SEO 标题';
COMMENT ON COLUMN page.seo_desc IS 'SEO 描述';
COMMENT ON COLUMN page.is_footer IS '是否页脚页：已发布且为真时随站点配置的 footer_pages 段下发给前台页脚';
COMMENT ON COLUMN page.created_at IS '创建时间';
COMMENT ON COLUMN page.updated_at IS '最近修改时间';

COMMENT ON TABLE ad IS '广告（docs/02 第 3.4 节）。曝光与点击先在 Valkey 累加，worker 每分钟落库';
COMMENT ON COLUMN ad.id IS '广告 ID';
COMMENT ON COLUMN ad.name IS '广告名（后台识别用），1–50 字';
COMMENT ON COLUMN ad.slot IS '广告位：global_popup 全站弹窗 / home_top / home_mid / home_bottom / footer_banner / list_feed 列表信息流 / detail_banner / preroll 片头 / pause 暂停 / immersive_feed 沉浸流 / text_link 文字链 / app_download';
COMMENT ON COLUMN ad.creative_type IS '素材类型：image 图片 / video 视频 / text 文字 / html 自定义。对应素材字段必填（表约束）';
COMMENT ON COLUMN ad.image IS '图片素材相对 key；视频素材时作为封面';
COMMENT ON COLUMN ad.video_url IS '视频素材地址（转码完成后指向 m3u8）';
COMMENT ON COLUMN ad.text IS '文字素材，不超过 200 字';
COMMENT ON COLUMN ad.html IS '自定义 HTML，只存 bluemonday 清洗后的结果，不超过 20000 字符；编辑需要 ads.custom_html 权限';
COMMENT ON COLUMN ad.alt IS '图片 / 视频的无障碍说明，不超过 100 字';
COMMENT ON COLUMN ad.click_url IS '跳转地址（站内路径或 http(s) 地址）；空串表示不跳转';
COMMENT ON COLUMN ad.device IS '投放设备：all 全部 / mobile 手机 / desktop 电脑';
COMMENT ON COLUMN ad.locales IS '投放语言；空数组表示全部语言';
COMMENT ON COLUMN ad.weight IS '同一位置内的出现机会（权重 1–100，默认 10）';
COMMENT ON COLUMN ad.end_at IS '结束投放时间；为空表示一直投放';
COMMENT ON COLUMN ad.freq_cap IS '每设备每天（北京时间）曝光上限（0–1000）；0 表示不限';
COMMENT ON COLUMN ad.status IS '状态：active 投放中 / inactive 已停投';
COMMENT ON COLUMN ad.imp_count IS '累计曝光数，由 worker 从 Valkey 回写';
COMMENT ON COLUMN ad.click_count IS '累计点击数，由 worker 从 Valkey 回写';
COMMENT ON COLUMN ad.created_at IS '创建时间';
COMMENT ON COLUMN ad.updated_at IS '最近修改时间；只在后台编辑时更新，计数落库不改';

COMMENT ON TABLE announcement IS '公告（docs/02 第 3.5 节）：顶部滚动条与弹窗。站内信（inbox）暂不启用，业务层拒绝新建';
COMMENT ON COLUMN announcement.id IS '公告 ID';
COMMENT ON COLUMN announcement.kind IS '形式：topbar 顶部条 / popup 弹窗 / inbox 站内信（暂不启用）';
COMMENT ON COLUMN announcement.content IS '公告正文，1–500 字';
COMMENT ON COLUMN announcement.click_url IS '点击跳转地址（站内路径或 http(s) 地址）';
COMMENT ON COLUMN announcement.frequency IS '展示频次：always 每次显示 / once 每人一次 / daily 每人每天一次（展示记录存客户端本机）';
COMMENT ON COLUMN announcement.pages IS '出现页面（home/explore/channel/tag/topic/drama/play/shua/rank/search/me）；空数组表示全部页面';
COMMENT ON COLUMN announcement.locales IS '投放语言；空数组表示全部语言';
COMMENT ON COLUMN announcement.image IS '弹窗配图（相对 key 或图片地址）';
COMMENT ON COLUMN announcement.start_at IS '开始投放时间；为空表示立即生效';
COMMENT ON COLUMN announcement.end_at IS '结束投放时间，必须晚于开始时间；为空表示一直投放';
COMMENT ON COLUMN announcement.status IS '状态：active 显示中 / inactive 已停用';
COMMENT ON COLUMN announcement.created_at IS '创建时间';
COMMENT ON COLUMN announcement.updated_at IS '最近修改时间';

COMMENT ON TABLE hotsearch IS '热搜词（docs/02 第 3.7 节）。搜索次数先在 Valkey 累加、worker 每小时落库；热搜榜取显示中的词，置顶在前，其余按次数倒序，共 10 个';
COMMENT ON COLUMN hotsearch.id IS '热搜词 ID';
COMMENT ON COLUMN hotsearch.word IS '规范化后的搜索词（全角转半角、去控制字符、转小写、合并空白），唯一，1–50 字';
COMMENT ON COLUMN hotsearch.search_count IS '累计搜索次数';
COMMENT ON COLUMN hotsearch.miss_count IS '搜了但片库没有结果的次数，用于发现缺片';
COMMENT ON COLUMN hotsearch.pinned IS '是否置顶（最多 10 个）；置顶的词一定显示（CHECK 约束）';
COMMENT ON COLUMN hotsearch.pin_sort IS '置顶顺序，从 1 开始；未置顶为 0';
COMMENT ON COLUMN hotsearch.visible IS '是否在热搜榜显示。auto 收录的词默认不显示，manual 添加的默认显示';
COMMENT ON COLUMN hotsearch.source IS '来源：manual 后台手动添加 / auto 按搜索行为自动收录';
COMMENT ON COLUMN hotsearch.last_searched_at IS '最近一次落库时有搜索的时间（按小时）';
COMMENT ON COLUMN hotsearch.created_at IS '创建时间';
COMMENT ON COLUMN hotsearch.updated_at IS '最近修改时间；只在后台修改时更新，计数落库不改';

COMMENT ON TABLE rank_override IS '排行人工干预（docs/02 第 3.8 节）。按榜整体保存，每榜最多 100 行；要重新排名才生效。线上榜单不在库里，由 worker 每 5 分钟算一次写入 Valkey 有序集合';
COMMENT ON COLUMN rank_override.board IS '榜单：drama_week 周榜 / drama_month 月榜';
COMMENT ON COLUMN rank_override.drama_id IS '目标剧；剧物理删除时一起删除';
COMMENT ON COLUMN rank_override.pinned IS '置顶：排在最前，多部置顶之间按分数。与 blocked 互斥';
COMMENT ON COLUMN rank_override.blocked IS '不让上榜，只影响该榜。与 pinned 互斥';
COMMENT ON COLUMN rank_override.boost IS '加分（-1000000000 到 1000000000），负数为减分，直接加到分数上';
COMMENT ON COLUMN rank_override.updated_at IS '最近调整时间';

-- ======================================================================
-- 5. 存储与媒体域（docs/12 第 7 节）
-- ======================================================================

COMMENT ON TABLE storage_provider IS '存储源（docs/12 第 2 节）：一个存储源 = 一个桶 + 一组凭据。凭据只写不读，读写与只读两套分开存';
COMMENT ON COLUMN storage_provider.id IS '存储源 ID';
COMMENT ON COLUMN storage_provider.name IS '存储源名称，1–64 字';
COMMENT ON COLUMN storage_provider.type IS '类型：s3 AWS S3 或 S3 兼容云存储 / r2 Cloudflare R2 / custom 自建 S3 兼容或只读 HTTP 源';
COMMENT ON COLUMN storage_provider.subtype IS '仅 custom 使用：s3_compatible 走 S3 协议 / http_origin 只读 HTTP 源（base_url 存入 endpoint）。其它类型留空';
COMMENT ON COLUMN storage_provider.purpose IS '用途：video 切片与清单 / image 封面等图片 / both 两者都放';
COMMENT ON COLUMN storage_provider.role IS '角色：primary 新内容写入 / mirror 异步复制 / readonly 只读历史资源';
COMMENT ON COLUMN storage_provider.endpoint IS '访问端点：s3 可选（S3 兼容服务填写）/ r2 由 account_id 自动生成 / s3_compatible 必填 / http_origin 存 base_url';
COMMENT ON COLUMN storage_provider.region IS '区域：s3 必填；r2 固定 auto（业务层补齐）';
COMMENT ON COLUMN storage_provider.bucket IS '桶名：s3 / r2 / s3_compatible 必填；http_origin 可空';
COMMENT ON COLUMN storage_provider.account_id IS 'Cloudflare 账号 ID，r2 必填';
COMMENT ON COLUMN storage_provider.path_style IS '是否使用 path-style 寻址（自建 MinIO / SeaweedFS 等通常需要）';
COMMENT ON COLUMN storage_provider.path_prefix IS '桶内前缀（如 media/），便于多环境共用一个桶';
COMMENT ON COLUMN storage_provider.credential_enc IS '读写凭据密文（信封加密后的 {access_key_id, secret_access_key}），用于上传、复制、删除；NULL 表示未配置';
COMMENT ON COLUMN storage_provider.read_credential_enc IS '只读凭据密文，用于生成预签名地址；NULL 表示未配置';
COMMENT ON COLUMN storage_provider.cred_tail IS '读写凭据 secret 的末 4 位，仅用于后台显示「已配置 ****abcd」（非敏感）';
COMMENT ON COLUMN storage_provider.read_cred_tail IS '只读凭据 secret 的末 4 位，仅用于后台显示';
COMMENT ON COLUMN storage_provider.max_presign_ttl IS '预签名最长有效期（秒），默认 600，上限 604800（S3/R2 协议上限 7 天）';
COMMENT ON COLUMN storage_provider.status IS '状态：active 正常 / draining 只读待迁出 / disabled 停用';
COMMENT ON COLUMN storage_provider.created_at IS '创建时间';
COMMENT ON COLUMN storage_provider.updated_at IS '最近修改时间';

COMMENT ON TABLE storage_line IS '存储线路（docs/12 第 3 节）：一个存储源可挂多条线路（CDN 域名 / 直连 / 自定义域名），按地区规则决定给哪些用户';
COMMENT ON COLUMN storage_line.id IS '线路 ID';
COMMENT ON COLUMN storage_line.provider_id IS '所属存储源；有线路的存储源删不掉（ON DELETE RESTRICT）';
COMMENT ON COLUMN storage_line.kind IS '承载内容：video 切片与清单 / image 封面等图片';
COMMENT ON COLUMN storage_line.name IS '线路名称，不超过 64 字；空串时前端显示「线路 N」';
COMMENT ON COLUMN storage_line.base_url IS '访问入口地址；presign 直连存储时留空，由存储源生成';
COMMENT ON COLUMN storage_line.access_mode IS '访问方式：presign 直接给存储预签名 / cdn 经 CDN 域名 / public 公开读（仅封面等）';
COMMENT ON COLUMN storage_line.sign_type IS '签名方式：s3_presign / hmac_token 目录前缀签名 / cdn_vendor CDN 厂商鉴权 / none 不签名';
COMMENT ON COLUMN storage_line.sign_key_ref IS '签名密钥引用（密钥本身在密钥管理中，不入库）';
COMMENT ON COLUMN storage_line.token_ttl IS '签名有效期（秒），60–604800';
COMMENT ON COLUMN storage_line.priority IS '优先级，数字越大越靠前';
COMMENT ON COLUMN storage_line.weight IS '同优先级内的流量权重（1–100）';
COMMENT ON COLUMN storage_line.region_scope IS '适用范围：rules 按地区规则命中 / global 兜底（所有地区可用）';
COMMENT ON COLUMN storage_line.video_process IS '历史兼容的产物形态：standard 标准 / text_to_image 旧混淆封装。新内容一律 standard';
COMMENT ON COLUMN storage_line.probe_path IS '健康探测文件路径，默认 /_probe/256k.bin';
COMMENT ON COLUMN storage_line.status IS '状态：active 正常 / watch 观察中（仍下发但排在后面）/ disabled 停用';
COMMENT ON COLUMN storage_line.note IS '运维备注，不超过 200 字';
COMMENT ON COLUMN storage_line.created_at IS '创建时间';
COMMENT ON COLUMN storage_line.updated_at IS '最近修改时间';

COMMENT ON TABLE storage_line_rule IS '线路地区规则（docs/12 第 3.2 节）：一条线路可有多条规则，命中任意一条即视为「本地区线路」';
COMMENT ON COLUMN storage_line_rule.id IS '规则 ID';
COMMENT ON COLUMN storage_line_rule.line_id IS '所属线路；线路删除时级联删除';
COMMENT ON COLUMN storage_line_rule.country IS '国家地区：ISO 3166-1 alpha-2 代码，* 表示全部';
COMMENT ON COLUMN storage_line_rule.province IS '省份，* 表示全部；仅中国大陆有意义';
COMMENT ON COLUMN storage_line_rule.isp IS '运营商（电信 / 联通 / 移动），* 表示全部';
COMMENT ON COLUMN storage_line_rule.asn IS '自治系统号，可选，用于精确匹配';
COMMENT ON COLUMN storage_line_rule.effect IS '作用：include 命中生效 / exclude 把某运营商排除出这条线路';
COMMENT ON COLUMN storage_line_rule.boost IS '命中后的额外加分（0–100），影响选线排序';

COMMENT ON TABLE storage_settings IS '存储全局设置（docs/12 第 7 节）：单行配置，始终存在，读取时无需处理「未初始化」';
COMMENT ON COLUMN storage_settings.id IS '固定为 1：CHECK 约束保证全表只有一行';
COMMENT ON COLUMN storage_settings.candidate_count IS '下发给客户端的候选线路数量（1–5，默认 3）';
COMMENT ON COLUMN storage_settings.default_ttl IS '默认签名有效期（秒），60–604800，默认 600';
COMMENT ON COLUMN storage_settings.smart_routing IS '是否启用智能选线（按 IP 地区与健康分排序）';
COMMENT ON COLUMN storage_settings.primary_video_provider_id IS '视频主存储：新视频写入此存储源';
COMMENT ON COLUMN storage_settings.primary_image_provider_id IS '图片主存储：新图片写入此存储源';
COMMENT ON COLUMN storage_settings.replicate_on_upload IS '上传完成后是否自动向 mirror 存储源复制';
COMMENT ON COLUMN storage_settings.updated_at IS '最近修改时间';

COMMENT ON TABLE media_object IS '媒体对象（docs/12 第 7 节）：存储中的一份产物。分集删除后记录保留（episode_id 置空），由清理任务删除物理文件';
COMMENT ON COLUMN media_object.id IS '媒体对象 ID';
COMMENT ON COLUMN media_object.kind IS '类型：hls_dir 一集 HLS 产物的目录（主清单、各档清单、s/ 下的切片）/ file 单个文件';
COMMENT ON COLUMN media_object.key IS '对象键，全局唯一；hls_dir 时为产物目录';
COMMENT ON COLUMN media_object.size_bytes IS '产物总字节数';
COMMENT ON COLUMN media_object.sha256 IS 'hls_dir 为主清单内容的 SHA-256；file 为文件内容的哈希';
COMMENT ON COLUMN media_object.episode_id IS '关联分集；分集删除时置空（ON DELETE SET NULL）';
COMMENT ON COLUMN media_object.created_at IS '创建时间';
COMMENT ON COLUMN media_object.orphaned_at IS '首次被发现不再被引用的时间；保留期（30 天）从它开始算，到期后连物理文件一起清理。用 created_at 算会让老产物一发现就被删';

COMMENT ON TABLE media_key IS 'HLS 内容密钥（docs/02 第 6.4 节）。每次转封装生成一把新的 16 字节 AES-128 密钥，key_ver 按分集从 1 递增；旧版本保留到旧产物清理';
COMMENT ON COLUMN media_key.episode_id IS '所属分集；分集删除时级联删除';
COMMENT ON COLUMN media_key.key_ver IS '密钥版本，按分集从 1 递增；episode.key_ver 指向当前产物使用的版本';
COMMENT ON COLUMN media_key.wrapped_key IS 'KEK 包装后的密钥密文：kid ‖ 0x3A ‖ nonce ‖ AES-256-GCM 密文，AAD 为 media_key|wrapped_key|<episode_id>|<key_ver>';
COMMENT ON COLUMN media_key.created_at IS '生成时间';

COMMENT ON TABLE media_replica IS '媒体副本（docs/12 第 5 节）：一个 media_object 在哪些存储源上有副本。选源只考虑 status=ready 的存储源';
COMMENT ON COLUMN media_replica.object_id IS '媒体对象；对象删除时级联删除';
COMMENT ON COLUMN media_replica.provider_id IS '存储源。不带 ON DELETE：有副本的存储源删不掉，由外键报错兜底';
COMMENT ON COLUMN media_replica.status IS '复制状态：pending 待复制 / ready 已就绪 / failed 失败';
COMMENT ON COLUMN media_replica.error IS '失败原因（给运营看，不含凭据）；成功后清空';
COMMENT ON COLUMN media_replica.replicated_at IS '复制完成时间';
COMMENT ON COLUMN media_replica.created_at IS '创建时间';
COMMENT ON COLUMN media_replica.updated_at IS '最近修改时间';

COMMENT ON TABLE transcode_job IS '转封装任务（docs/02 第 4.1a 节）。一次提交一行，自动重试沿用同一行；部分唯一索引保证同一集同时最多一个 queued / running 任务';
COMMENT ON COLUMN transcode_job.id IS '任务编号（UUIDv7，对外暴露），按时间有序';
COMMENT ON COLUMN transcode_job.episode_id IS '目标分集；分集删除时级联删除';
COMMENT ON COLUMN transcode_job.source_key IS '源文件在 MEDIA_SOURCE_BUCKET 中的路径';
COMMENT ON COLUMN transcode_job.force IS '是否强制重转：为真时即使 source_hash 相同也重新转码';
COMMENT ON COLUMN transcode_job.status IS '状态：queued 排队或等待重试 / running 进行中 / succeeded 成功 / skipped 源文件与当前产物相同未重转 / failed 失败';
COMMENT ON COLUMN transcode_job.progress IS '进度百分比（0–100）';
COMMENT ON COLUMN transcode_job.attempts IS '已尝试次数';
COMMENT ON COLUMN transcode_job.error IS '最近一次失败原因';
COMMENT ON COLUMN transcode_job.source_hash IS '源文件 SHA-256，与 episode.source_hash 比较以判定 skipped';
COMMENT ON COLUMN transcode_job.output_object_id IS '产出的媒体对象（media_object）；对象删除时置空';
COMMENT ON COLUMN transcode_job.key_ver IS '本次产物使用的内容密钥版本';
COMMENT ON COLUMN transcode_job.renditions IS '本次产物的码率阶梯，成功后写入 episode.renditions';
COMMENT ON COLUMN transcode_job.poster IS '本次生成的首帧图相对 key';
COMMENT ON COLUMN transcode_job.duration_sec IS '探测出的时长（秒）';
COMMENT ON COLUMN transcode_job.created_at IS '入队时间';
COMMENT ON COLUMN transcode_job.started_at IS '开始执行时间';
COMMENT ON COLUMN transcode_job.finished_at IS '结束时间（成功、跳过或失败）';
COMMENT ON COLUMN transcode_job.updated_at IS '最近更新时间（含进度刷新）';

COMMENT ON TABLE admin_media_upload IS '运营后台媒体上传会话（docs/02 第 4.1b 节）。浏览器只通过受后台会话、CSRF 与所有者校验的同源接口上传；会话不保存凭据，仍由服务端从 provider 当前密文解密读写';
COMMENT ON COLUMN admin_media_upload.id IS '会话 ID（UUIDv7）';
COMMENT ON COLUMN admin_media_upload.admin_id IS '创建者（admin_user）；只有创建者能继续该会话';
COMMENT ON COLUMN admin_media_upload.provider_id IS '创建时选中的存储源；后续主存储切换不影响已开始的会话';
COMMENT ON COLUMN admin_media_upload.provider_endpoint IS '创建时快照的存储端点，避免运营改连接参数后对象写错位置';
COMMENT ON COLUMN admin_media_upload.provider_region IS '创建时快照的存储区域';
COMMENT ON COLUMN admin_media_upload.provider_bucket IS '创建时快照的桶名';
COMMENT ON COLUMN admin_media_upload.provider_path_style IS '创建时快照的 path-style 寻址开关';
COMMENT ON COLUMN admin_media_upload.provider_path_prefix IS '创建时快照的桶内前缀';
COMMENT ON COLUMN admin_media_upload.purpose IS '用途：drama_cover 剧封面 / episode_cover 分集封面 / announcement_image 公告配图 / ad_image 广告图 / ad_poster 广告封面 / feedback_project_cover 内测项目封面 / static_page_image 静态页配图 / episode_video 分集视频 / ad_video 广告视频';
COMMENT ON COLUMN admin_media_upload.kind IS '媒体种类：image 图片 / video 视频';
COMMENT ON COLUMN admin_media_upload.mime IS '客户端声明的 MIME 类型，服务端另行校验';
COMMENT ON COLUMN admin_media_upload.image_format IS '图片格式：webp / avif；视频时为空串。当前上传只接受 webp';
COMMENT ON COLUMN admin_media_upload.size_bytes IS '文件字节数，必须大于 0';
COMMENT ON COLUMN admin_media_upload.source_key IS '源对象键，全局唯一：图片写 img/<purpose>/，视频写 video/source/';
COMMENT ON COLUMN admin_media_upload.output_key IS '产物对象键：图片与源相同；广告视频完成后指向 video/assets/<id>/master.m3u8';
COMMENT ON COLUMN admin_media_upload.episode_id IS '分集视频上传时的目标分集；分集删除时置空';
COMMENT ON COLUMN admin_media_upload.transcode_job_id IS '分集视频关联的转封装任务；任务删除时置空';
COMMENT ON COLUMN admin_media_upload.status IS '状态：created 已创建 / uploading 已领取写入权 / uploaded 已上传 / queued 已入队转码 / running 转码中 / ready 可用 / failed 失败';
COMMENT ON COLUMN admin_media_upload.progress IS '进度百分比（0–100）';
COMMENT ON COLUMN admin_media_upload.attempts IS '已尝试次数';
COMMENT ON COLUMN admin_media_upload.error IS '失败原因';
COMMENT ON COLUMN admin_media_upload.expires_at IS '会话过期时间；过期后由清理任务删除未完成的对象';
COMMENT ON COLUMN admin_media_upload.created_at IS '创建时间';
COMMENT ON COLUMN admin_media_upload.updated_at IS '最近更新时间';

COMMENT ON TABLE ftp_server IS '资源机 FTP（docs/02 第 4.2 节）。账号密码不入库，由 worker 从环境变量读取。设置类数据，删除不进回收站';
COMMENT ON COLUMN ftp_server.id IS '资源机 ID';
COMMENT ON COLUMN ftp_server.name IS '资源机名称，1–64 字';
COMMENT ON COLUMN ftp_server.host IS '域名或 IP，统一存小写';
COMMENT ON COLUMN ftp_server.port IS '端口，默认 21';
COMMENT ON COLUMN ftp_server.tls IS '加密方式：none 不加密 / explicit 显式 TLS（AUTH TLS）/ implicit 隐式 TLS。非开发环境只允许 explicit';
COMMENT ON COLUMN ftp_server.passive IS '是否被动模式。目前只支持 true：主动模式需要资源机回连 worker';
COMMENT ON COLUMN ftp_server.cred_ref IS '凭据引用（大写字母开头，≤32 位，可多台共用）：worker 读环境变量 FTP_<cred_ref>_USER / FTP_<cred_ref>_PASSWORD';
COMMENT ON COLUMN ftp_server.status IS '状态：active 启用 / inactive 停用';
COMMENT ON COLUMN ftp_server.created_at IS '创建时间';
COMMENT ON COLUMN ftp_server.updated_at IS '最近修改时间';

COMMENT ON TABLE resource_check IS '资源体检结果（docs/02 第 4.3 节）：批量探测视频产物与封面是否可达。一个资源只留最后一次结果（UPSERT）';
COMMENT ON COLUMN resource_check.id IS '记录 ID';
COMMENT ON COLUMN resource_check.drama_id IS '所属剧；剧删除时级联删除';
COMMENT ON COLUMN resource_check.episode_id IS '所属分集；剧封面的记录没有分集（为空）';
COMMENT ON COLUMN resource_check.file_type IS '资源大类：video 视频 / cover 图片';
COMMENT ON COLUMN resource_check.field IS '具体字段：cover_portrait 竖封面 / cover_landscape 横封面 / episode_cover 分集封面 / episode_poster 首帧图 / video 视频。只有 file_type 分不出该改哪个字段';
COMMENT ON COLUMN resource_check.url IS '探测地址，不含查询串：HMAC 令牌与 S3 预签名带签名材料，落库等于把密钥派生值留在表里。探测时按线路现算签名';
COMMENT ON COLUMN resource_check.line_id IS '本次探测使用的存储线路；同一对象键经不同线路的可达性可能不同，记下来结果才解释得通';
COMMENT ON COLUMN resource_check.result IS '结果：ok 正常 / not_found 404 / forbidden 403 / server_error 5xx / unopenable 响应异常打不开 / unreachable 连不上';
COMMENT ON COLUMN resource_check.status_code IS 'HTTP 状态码；连不上时为 0';
COMMENT ON COLUMN resource_check.elapsed_ms IS '耗时（毫秒），用于区分超时与直接拒绝';
COMMENT ON COLUMN resource_check.message IS '补充说明（DNS 失败、超时、被出站策略拦截等）';
COMMENT ON COLUMN resource_check.checked_at IS '本次探测时间';

COMMENT ON TABLE resource_warm_job IS '资源预热任务（docs/02 第 4.4 节）：worker 预先拉取清单与前 N 个切片让 CDN 边缘缓存住。同一时间仅一轮（部分唯一索引）';
COMMENT ON COLUMN resource_warm_job.id IS '任务 ID';
COMMENT ON COLUMN resource_warm_job.drama_id IS '预热范围；NULL 表示全部剧。剧删除后保留记录，置空即可';
COMMENT ON COLUMN resource_warm_job.mode IS '模式：playlist_only 只热清单 / with_segments 清单加前若干切片';
COMMENT ON COLUMN resource_warm_job.segments_per_quality IS '每个清晰度拉前几个切片（1–20），with_segments 时有效';
COMMENT ON COLUMN resource_warm_job.concurrency IS '同时热几集（1–16）';
COMMENT ON COLUMN resource_warm_job.include_unpublished IS '是否包含未上架的剧';
COMMENT ON COLUMN resource_warm_job.status IS '状态：running 进行中 / done 完成 / stopped 运营停止 / interrupted 心跳超时判定中断';
COMMENT ON COLUMN resource_warm_job.episodes_total IS '计划预热的分集总数（进度条分母）';
COMMENT ON COLUMN resource_warm_job.episodes_done IS '已处理完的分集数';
COMMENT ON COLUMN resource_warm_job.warmed IS '热好的资源数（清单与切片各算一个）';
COMMENT ON COLUMN resource_warm_job.failed IS '拿不到的资源数';
COMMENT ON COLUMN resource_warm_job.fetched IS '实际发出的请求数';
COMMENT ON COLUMN resource_warm_job.traffic_bytes IS '累计拉取流量（字节）';
COMMENT ON COLUMN resource_warm_job.current IS '当前在热什么，给运营看进展（如「第 12 集 · 720p」）';
COMMENT ON COLUMN resource_warm_job.stop_requested IS '停止信号：运营点「停止」时置真。用列传信号比 Valkey 可靠（worker 重启后信号还在）';
COMMENT ON COLUMN resource_warm_job.error IS '中断原因';
COMMENT ON COLUMN resource_warm_job.admin_id IS '发起人。不加外键：任务记录要比管理员账号活得久';
COMMENT ON COLUMN resource_warm_job.started_at IS '开始时间';
COMMENT ON COLUMN resource_warm_job.heartbeat_at IS 'worker 心跳时间；超时即判定为 interrupted（worker 崩了之后没人改 status）';
COMMENT ON COLUMN resource_warm_job.finished_at IS '结束时刻。存时刻而不是时长，运行中也能算出已跑多久';

COMMENT ON TABLE import_log IS '批量导入批次（docs/06 第 8 节，A4-2 / S4-6）：只记正式导入，预览不写这张表';
COMMENT ON COLUMN import_log.id IS '批次 ID';
COMMENT ON COLUMN import_log.source IS '来源标识，运营填写，会写进 drama.source（采集批次）';
COMMENT ON COLUMN import_log.file_name IS '导入的文件名，只用于让运营认出是哪一批';
COMMENT ON COLUMN import_log.rows_total IS '文件总行数';
COMMENT ON COLUMN import_log.dramas_created IS '新建的剧数';
COMMENT ON COLUMN import_log.dramas_updated IS '更新的剧数';
COMMENT ON COLUMN import_log.episodes_created IS '新建的分集数。CSV 导入恒为 0（CSV 列里没有分集信息），保留以便与 FTP 扫描、开放平台推送共用同一套口径';
COMMENT ON COLUMN import_log.episodes_updated IS '更新的分集数。CSV 导入恒为 0，同上';
COMMENT ON COLUMN import_log.skipped IS '跳过行数：行本身没法用（剧名为空）或文件内重复';
COMMENT ON COLUMN import_log.rejected IS '被拒行数：命中屏蔽词（采集只认 block 动作）';
COMMENT ON COLUMN import_log.tags_created IS '自动新建的标签数';
COMMENT ON COLUMN import_log.admin_id IS '操作人。不加外键：批次记录要比管理员账号活得久';
COMMENT ON COLUMN import_log.created_at IS '导入时间';

-- ======================================================================
-- 6. 用户与会话域（user / like 是 SQL 保留字，表名与列名需加双引号）
-- ======================================================================

COMMENT ON TABLE "user" IS '注册用户（docs/02 第 5.1 节）。不提供找回密码（adr/0001），因此不存恢复码、邮箱、手机号；注销的用户名也不释放';
COMMENT ON COLUMN "user".id IS '用户 ID';
COMMENT ON COLUMN "user".username IS '登录名：统一存小写，3–32 位 [a-z0-9_]，大小写不敏感唯一';
COMMENT ON COLUMN "user".nickname IS '展示昵称，不超过 40 字';
COMMENT ON COLUMN "user".password_hash IS '密码哈希（Argon2id PHC 串）';
COMMENT ON COLUMN "user".avatar IS '头像相对 key';
COMMENT ON COLUMN "user".bio IS '个人简介，不超过 80 字';
COMMENT ON COLUMN "user".nickname_status IS '昵称审核状态：ok 通过 / pending 待审 / rejected 驳回';
COMMENT ON COLUMN "user".bio_status IS '简介审核状态：ok 通过 / pending 待审 / rejected 驳回';
COMMENT ON COLUMN "user".avatar_status IS '头像审核状态：ok 通过 / pending 待审 / rejected 驳回';
COMMENT ON COLUMN "user".nickname_changed_at IS '近 30 天的改名时间记录，用于限制改名频次（docs/13 第 2.5 节）';
COMMENT ON COLUMN "user".locale IS '界面语言偏好；为空表示跟随客户端';
COMMENT ON COLUMN "user".history_paused IS '是否暂停记录观看历史';
COMMENT ON COLUMN "user".status IS '账号状态：normal 正常 / muted 禁言 / banned 封禁';
COMMENT ON COLUMN "user".verified_at IS '实名或手机号验证时间，按运营地开关启用';
COMMENT ON COLUMN "user".created_at IS '注册时间';
COMMENT ON COLUMN "user".deleted_at IS '软删除时间：非空表示在回收站，其评论一并进回收站';

COMMENT ON TABLE guest_device IS '游客设备（docs/02 第 5.1 节）：设备级游客身份，服务端生成 UUIDv7，不存任何硬件标识';
COMMENT ON COLUMN guest_device.id IS '设备 ID（UUIDv7），由服务端生成';
COMMENT ON COLUMN guest_device.first_seen IS '首次出现时间，后台游客列表按它倒序';
COMMENT ON COLUMN guest_device.source_channel IS '来源渠道标识（推广落地页等）';
COMMENT ON COLUMN guest_device.platform IS '平台：web / ios / android';
COMMENT ON COLUMN guest_device.attest_level IS '设备可信度：none 无 / basic 基础 / strong 强（APP 设备证明）';
COMMENT ON COLUMN guest_device.merged_user_id IS '登录后并入的账号；账号删除时置空。仅用于统计';
COMMENT ON COLUMN guest_device.created_at IS '记录创建时间';

COMMENT ON TABLE user_session IS '刷新令牌轮换族（docs/02 第 5.1.1 节）。刷新时新建行并吊销旧行；旧令牌复用即判定泄露，吊销整族。服务端只存 refresh 的 SHA-256';
COMMENT ON COLUMN user_session.id IS '会话 ID（UUIDv7）';
COMMENT ON COLUMN user_session.family_id IS '轮换族 ID：同一次登录的历次刷新共享，判定泄露时按它整族吊销';
COMMENT ON COLUMN user_session.user_id IS '注册用户；与 guest_device_id 恰好二选一（CHECK 约束）';
COMMENT ON COLUMN user_session.guest_device_id IS '游客设备；与 user_id 恰好二选一（CHECK 约束）';
COMMENT ON COLUMN user_session.refresh_hash IS 'refresh 令牌的 SHA-256，唯一；明文不入库';
COMMENT ON COLUMN user_session.ua_digest IS 'User-Agent 摘要，不存原始 UA';
COMMENT ON COLUMN user_session.ip_prefix IS '来源 IP 网段，不存完整 IP';
COMMENT ON COLUMN user_session.platform IS '平台：web / ios / android';
COMMENT ON COLUMN user_session.created_at IS '创建时间';
COMMENT ON COLUMN user_session.last_seen_at IS '最近一次使用时间，「我的设备」列表展示';
COMMENT ON COLUMN user_session.expires_at IS '过期时间；清理任务按它批量删除';
COMMENT ON COLUMN user_session.revoked_at IS '吊销时间；非空表示已失效';
COMMENT ON COLUMN user_session.revoke_reason IS '吊销原因（refresh_rotated / password_changed / family_compromised 等）';

COMMENT ON TABLE watch_history IS '观看历史（docs/02 第 5.2 节）：续播断点，每人每剧一行，保留最近 500 部。只存注册用户，游客历史存本机、登录后经 /me/local-merge 并入。高频上报先写 Valkey、worker 每分钟批量 UPSERT';
COMMENT ON COLUMN watch_history.user_id IS '用户；用户删除时级联删除';
COMMENT ON COLUMN watch_history.drama_id IS '剧集；剧物理删除时级联删除';
COMMENT ON COLUMN watch_history.episode_id IS '最近看的分集 ID（不建外键，分集删除后仍保留断点）';
COMMENT ON COLUMN watch_history.episode_number IS '最近看的集号，用于「看到第 N 集」展示';
COMMENT ON COLUMN watch_history.position_sec IS '断点位置（秒）';
COMMENT ON COLUMN watch_history.duration_sec IS '该集总时长（秒），用于算进度条';
COMMENT ON COLUMN watch_history.updated_at IS '最近上报时间；续播列表按它倒序';

COMMENT ON TABLE follow IS '收藏（追剧，docs/02 第 5.3 节）。只存注册用户，游客收藏存本机、不计入公开数';
COMMENT ON COLUMN follow.user_id IS '用户；用户删除时级联删除';
COMMENT ON COLUMN follow.drama_id IS '剧集；剧物理删除时级联删除';
COMMENT ON COLUMN follow.created_at IS '收藏时间；列表按它倒序';

COMMENT ON TABLE "like" IS '点赞（docs/02 第 5.3 节）：可点赞剧或分集。只存注册用户，游客点赞存本机、不计入公开数。like 是 SQL 保留字，查询需加双引号';
COMMENT ON COLUMN "like".user_id IS '用户；用户删除时级联删除';
COMMENT ON COLUMN "like".target_type IS '目标类型：drama 剧 / episode 分集';
COMMENT ON COLUMN "like".target_id IS '目标主键，按 target_type 多态（不建外键）';
COMMENT ON COLUMN "like".created_at IS '点赞时间；列表按它倒序';

COMMENT ON TABLE notification IS '站内通知（docs/02 第 5.9 节）';
COMMENT ON COLUMN notification.id IS '通知 ID';
COMMENT ON COLUMN notification.user_id IS '接收用户；用户删除时级联删除';
COMMENT ON COLUMN notification.type IS '类型：reply 评论被回复 / like 被点赞 / update 追更';
COMMENT ON COLUMN notification.payload IS '跳转所需的上下文（评论 / 剧 / 集等标识与摘要）';
COMMENT ON COLUMN notification.is_read IS '是否已读；未读数走部分索引';
COMMENT ON COLUMN notification.created_at IS '产生时间；列表按它倒序';

-- ======================================================================
-- 7. 社区域：评论、弹幕、举报、屏蔽词、表情
-- （评论与弹幕的作者多态：user_id 或 guest_device_id 恰好一个）
-- ======================================================================

COMMENT ON TABLE comment IS '评论（docs/02 第 5.4 节）。状态机 pending 待审 → visible 显示 / hidden 隐藏；先审后显由 site_config.comment_policy 或命中 review 屏蔽词触发';
COMMENT ON COLUMN comment.id IS '评论 ID';
COMMENT ON COLUMN comment.episode_id IS '所属分集；分集删除时级联删除';
COMMENT ON COLUMN comment.user_id IS '注册用户作者；与 guest_device_id 恰好二选一（CHECK 约束）';
COMMENT ON COLUMN comment.guest_device_id IS '游客作者；与 user_id 恰好二选一（CHECK 约束）';
COMMENT ON COLUMN comment.parent_id IS '被回复的评论；为空表示顶层评论';
COMMENT ON COLUMN comment.content IS '评论正文，1–500 字';
COMMENT ON COLUMN comment.image_count IS '附图数量；带图评论整条先审后显';
COMMENT ON COLUMN comment.status IS '状态：pending 待审 / visible 显示 / hidden 隐藏';
COMMENT ON COLUMN comment.like_count IS '点赞数；先在 Valkey 累加后回写';
COMMENT ON COLUMN comment.created_at IS '发表时间';
COMMENT ON COLUMN comment.deleted_at IS '软删除时间：非空表示在回收站';

COMMENT ON TABLE comment_like IS '评论点赞。仅注册用户，游客点赞存本机、不计入公开数';
COMMENT ON COLUMN comment_like.comment_id IS '评论；评论删除时级联删除';
COMMENT ON COLUMN comment_like.user_id IS '用户；用户删除时级联删除';
COMMENT ON COLUMN comment_like.created_at IS '点赞时间';

COMMENT ON TABLE danmaku IS '弹幕（docs/02 第 5.5 节）。播放时按 60 秒窗口按 time_ms 拉取显示中的弹幕';
COMMENT ON COLUMN danmaku.id IS '弹幕 ID';
COMMENT ON COLUMN danmaku.episode_id IS '所属分集；分集删除时级联删除';
COMMENT ON COLUMN danmaku.user_id IS '注册用户作者；与 guest_device_id 恰好二选一（CHECK 约束）';
COMMENT ON COLUMN danmaku.guest_device_id IS '游客作者；与 user_id 恰好二选一（CHECK 约束）';
COMMENT ON COLUMN danmaku.time_ms IS '出现时间（毫秒，相对视频起点）';
COMMENT ON COLUMN danmaku.content IS '弹幕正文，1–40 字';
COMMENT ON COLUMN danmaku.color IS '颜色 RRGGBB，默认 FFFFFF；可选值白名单在领域层';
COMMENT ON COLUMN danmaku.status IS '状态：pending 待审 / visible 显示 / hidden 隐藏';
COMMENT ON COLUMN danmaku.created_at IS '发表时间';

COMMENT ON TABLE report IS '举报（docs/02 第 5.6 节）。24 小时内同设备对同对象只计一次由 Valkey 去重，不建唯一约束';
COMMENT ON COLUMN report.id IS '举报 ID';
COMMENT ON COLUMN report.target_type IS '举报对象类型：comment 评论 / danmaku 弹幕 / drama 剧 / comment_image 评论配图';
COMMENT ON COLUMN report.target_id IS '举报对象主键，按 target_type 多态（不建外键）';
COMMENT ON COLUMN report.reporter_id IS '注册用户举报人；账号删除时置空';
COMMENT ON COLUMN report.reporter_guest_id IS '游客举报人；设备删除时置空';
COMMENT ON COLUMN report.reason IS '原因：attack 人身攻击 / vulgar 低俗 / illegal 违法 / minor 涉未成年 / ad 广告 / spoiler 剧透 / other 其他';
COMMENT ON COLUMN report.detail IS '举报补充说明，不超过 500 字';
COMMENT ON COLUMN report.status IS '处理状态：pending 待处理 / resolved 已处理 / rejected 已驳回';
COMMENT ON COLUMN report.handle_note IS '运营处理备注';
COMMENT ON COLUMN report.created_at IS '举报时间';

COMMENT ON TABLE blockword IS '屏蔽词（docs/02 第 5.7 节）。引擎用 Aho-Corasick 自动机进程内常驻，变更后重建并经 Valkey Pub/Sub 通知各实例。改处理办法 = 先删再加（word 唯一），所以没有更新接口';
COMMENT ON COLUMN blockword.id IS '屏蔽词 ID';
COMMENT ON COLUMN blockword.word IS '规范化后的词（全角转半角、转小写、去零宽字符与常见插入符号），唯一，不超过 100 字；匹配时对文本做同样规范化';
COMMENT ON COLUMN blockword."group" IS '分组，如「引流」用于识别简介与评论中的联系方式。group 是 SQL 保留字，查询需加双引号';
COMMENT ON COLUMN blockword.action IS '命中处理：block 直接拦下 / mask 替换成 *** / review 先送审。适用于采集剧名、评论、弹幕、昵称、简介；昵称上 mask 不生效，采集只认 block';
COMMENT ON COLUMN blockword.created_at IS '加入时间';

COMMENT ON TABLE emoji_pack IS '表情包（docs/02 第 5.8 节）：表情的分组';
COMMENT ON COLUMN emoji_pack.id IS '表情包 ID';
COMMENT ON COLUMN emoji_pack.name IS '表情包名称';
COMMENT ON COLUMN emoji_pack.sort IS '展示顺序';
COMMENT ON COLUMN emoji_pack.created_at IS '创建时间';

COMMENT ON TABLE emoji IS '表情（docs/02 第 5.8 节）。供前台 GET /api/v1/emojis 下发，评论里按 code 渲染';
COMMENT ON COLUMN emoji.id IS '表情 ID';
COMMENT ON COLUMN emoji.pack_id IS '所属表情包；表情包删除时级联删除';
COMMENT ON COLUMN emoji.code IS '评论里的表情码，如 [微笑]，全站唯一';
COMMENT ON COLUMN emoji.image IS '表情图片相对 key';
COMMENT ON COLUMN emoji.sort IS '包内展示顺序';
COMMENT ON COLUMN emoji.created_at IS '创建时间';

-- ======================================================================
-- 8. 会员与支付域（docs/02 第 5.10 节、adr/0004）
-- 金额一律用整数最小单位存：人民币为分，USDT 为 10⁻⁶ USDT
-- ======================================================================

COMMENT ON TABLE vip_plan IS '会员套餐（docs/02 第 5.10 节）：五种卡，code 唯一，名称按 code 走多语言文案。种子数据均为 off 且金额为 0，等后台配置真实价格后才能上架';
COMMENT ON COLUMN vip_plan.id IS '套餐 ID';
COMMENT ON COLUMN vip_plan.code IS '套餐编码：day 日卡 / week 周卡 / month 月卡 / quarter 季卡 / year 年卡，唯一';
COMMENT ON COLUMN vip_plan.duration_days IS '会员时长（天），默认 1 / 7 / 30 / 90 / 365';
COMMENT ON COLUMN vip_plan.price_fen IS '售价（人民币分）';
COMMENT ON COLUMN vip_plan.list_price_fen IS '划线价（人民币分），用于展示折扣';
COMMENT ON COLUMN vip_plan.price_micro_usdt IS 'USDT 售价（10⁻⁶ USDT，与 TRC-20/ERC-20 合约的 6 位小数一致）';
COMMENT ON COLUMN vip_plan.badge_key IS '角标文案键（走多语言），可空';
COMMENT ON COLUMN vip_plan.recommended IS '是否标为推荐';
COMMENT ON COLUMN vip_plan.channels IS '允许的支付渠道：alipay / wechat / usdt / iap_apple / iap_google';
COMMENT ON COLUMN vip_plan.clients IS '允许的客户端：web_pc / web_mobile / app_official / app_store';
COMMENT ON COLUMN vip_plan.sort IS '展示顺序（0–9999）';
COMMENT ON COLUMN vip_plan.status IS '状态：on 已上架 / off 已下架（不向用户显示或开放购买）';
COMMENT ON COLUMN vip_plan.updated_at IS '最近修改时间';

COMMENT ON TABLE user_vip IS '会员状态（docs/02 第 5.10 节）：一位用户一行。判定会员为 expire_at > now()；读路径走 Valkey 缓存，变更时失效';
COMMENT ON COLUMN user_vip.user_id IS '用户；用户删除时级联删除';
COMMENT ON COLUMN user_vip.expire_at IS '会员到期时间；续费按 max(expire_at, now()) 顺延';
COMMENT ON COLUMN user_vip.total_days IS '累计获得的会员天数';
COMMENT ON COLUMN user_vip.first_paid_at IS '首次付费时间；赠送开通时为空';
COMMENT ON COLUMN user_vip.updated_at IS '最近变更时间';

COMMENT ON TABLE vip_ledger IS '会员流水（docs/02 第 5.10 节）：只追加，不修改。user_vip 的每次变动都要写一条；会员迁移写一对 migrate_out / migrate_in';
COMMENT ON COLUMN vip_ledger.id IS '流水 ID';
COMMENT ON COLUMN vip_ledger.user_id IS '用户；用户删除时级联删除';
COMMENT ON COLUMN vip_ledger.order_id IS '关联订单（pay_order）；订单删除时置空，流水保留';
COMMENT ON COLUMN vip_ledger.change_days IS '变动天数，可为负（退款、后台收回）；不允许为 0';
COMMENT ON COLUMN vip_ledger.from_expire IS '变动前的到期时间';
COMMENT ON COLUMN vip_ledger.to_expire IS '变动后的到期时间';
COMMENT ON COLUMN vip_ledger.reason IS '变动原因：purchase 购买 / refund 退款 / admin_grant 后台赠送 / admin_revoke 后台收回 / migrate_in 迁入 / migrate_out 迁出 / compensation 补偿 / reward 活动奖励 / mall 商城兑换';
COMMENT ON COLUMN vip_ledger.admin_id IS '操作管理员（后台赠送 / 收回时）；账号删除时置空';
COMMENT ON COLUMN vip_ledger.note IS '备注，不超过 500 字';
COMMENT ON COLUMN vip_ledger.created_at IS '发生时间';

COMMENT ON TABLE payment_channel IS '支付渠道配置（docs/02 第 5.10 节）。凭据只以信封密文保存、后台只写不读；初始全部关闭，修改凭据需超管二次验证';
COMMENT ON COLUMN payment_channel.channel IS '渠道：alipay 支付宝 / wechat 微信 / usdt 链上转账 / iap_apple 苹果内购 / iap_google 谷歌内购';
COMMENT ON COLUMN payment_channel.enabled IS '是否启用；未配置真实价格与凭据时不能发起支付';
COMMENT ON COLUMN payment_channel.clients IS '允许的客户端：web_pc / web_mobile / app_official / app_store';
COMMENT ON COLUMN payment_channel.regions_allow IS '允许地区（ISO 国家码）；空数组表示不限制';
COMMENT ON COLUMN payment_channel.regions_deny IS '禁止地区（ISO 国家码）';
COMMENT ON COLUMN payment_channel.config IS '非机密配置：商户号、AppID、网关地址等';
COMMENT ON COLUMN payment_channel.credential_enc IS '凭据密文（私钥、APIv3 密钥、证书等信封加密）；需独立 KEK，不复用存储凭据密钥';
COMMENT ON COLUMN payment_channel.updated_at IS '最近修改时间';

COMMENT ON TABLE pay_order IS '支付订单（docs/02 第 5.10 节）。状态机 pending → paid（验签通过且金额一致）/ pending → closed（超时或取消）/ paid → refunded。金额与商品快照在创建时固定，后续改价不影响已有订单';
COMMENT ON COLUMN pay_order.id IS '订单 ID（内部）';
COMMENT ON COLUMN pay_order.order_no IS '对外订单号，随机不可推测，20–64 位，唯一';
COMMENT ON COLUMN pay_order.user_id IS '下单用户；有订单的用户不能物理删除（ON DELETE RESTRICT）';
COMMENT ON COLUMN pay_order.product_type IS '商品类型：vip_plan 会员套餐 / coin_package 金币包';
COMMENT ON COLUMN pay_order.product_id IS '商品主键（按 product_type 指向 vip_plan 或 coin_package）';
COMMENT ON COLUMN pay_order.product_snapshot IS '下单时的商品快照（天数 / 金币数与价格），后续改价不影响本单';
COMMENT ON COLUMN pay_order.coupon_id IS '使用的优惠券；无则为空';
COMMENT ON COLUMN pay_order.discount_fen IS '优惠金额（人民币分）';
COMMENT ON COLUMN pay_order.channel IS '支付渠道（payment_channel）';
COMMENT ON COLUMN pay_order.client IS '下单客户端：web_pc / web_mobile / app_official / app_store';
COMMENT ON COLUMN pay_order.currency IS '币种：CNY 人民币 / USDT';
COMMENT ON COLUMN pay_order.amount IS '应付金额，按 currency 取最小单位（CNY 为分，USDT 为 10⁻⁶）';
COMMENT ON COLUMN pay_order.status IS '状态：pending 待支付 / paid 已支付 / closed 已关闭 / refunded 已退款';
COMMENT ON COLUMN pay_order.channel_trade_no IS '渠道流水号或链上交易哈希；与 channel 组成唯一索引，防重复到账';
COMMENT ON COLUMN pay_order.paid_at IS '支付成功时间';
COMMENT ON COLUMN pay_order.expire_at IS '订单失效时间，默认下单后 30 分钟';
COMMENT ON COLUMN pay_order.late IS '关闭后才到账：照常发放会员并标记为真，进入人工复核';
COMMENT ON COLUMN pay_order.refund_amount IS '已退金额，口径同 amount';
COMMENT ON COLUMN pay_order.idempotency_key IS '下单幂等键；与 user_id 组成唯一约束，防重复下单';
COMMENT ON COLUMN pay_order.created_at IS '下单时间';
COMMENT ON COLUMN pay_order.updated_at IS '最近更新时间';

COMMENT ON TABLE pay_notify_log IS '支付回调与链上事件留痕（docs/02 第 5.10 节）：供对账与排障，保留 180 天。原文只存摘要与密文，禁止写入日志';
COMMENT ON COLUMN pay_notify_log.id IS '记录 ID';
COMMENT ON COLUMN pay_notify_log.channel IS '来源渠道';
COMMENT ON COLUMN pay_notify_log.order_no IS '回调中解析出的对外订单号；解析失败时为空串';
COMMENT ON COLUMN pay_notify_log.channel_trade_no IS '渠道流水号或交易哈希';
COMMENT ON COLUMN pay_notify_log.verified IS '验签是否通过';
COMMENT ON COLUMN pay_notify_log.amount IS '回调声明的金额，口径同 pay_order.amount';
COMMENT ON COLUMN pay_notify_log.raw_digest IS '回调原文的 SHA-256（固定 32 字节），用于去重与比对';
COMMENT ON COLUMN pay_notify_log.raw_enc IS '回调原文的信封密文；只在排障时由授权流程解密';
COMMENT ON COLUMN pay_notify_log.received_at IS '接收时间';
COMMENT ON COLUMN pay_notify_log.handled_result IS '处理结果说明，不超过 200 字';

COMMENT ON TABLE pay_reconcile_daily IS '每日对账结果（docs/02 第 5.10 节）：本地已支付订单与渠道账单的比对';
COMMENT ON COLUMN pay_reconcile_daily.day IS '对账日期';
COMMENT ON COLUMN pay_reconcile_daily.channel IS '支付渠道（payment_channel）';
COMMENT ON COLUMN pay_reconcile_daily.orders_paid IS '本地已支付订单数';
COMMENT ON COLUMN pay_reconcile_daily.amount_paid IS '本地已支付金额';
COMMENT ON COLUMN pay_reconcile_daily.bill_count IS '渠道账单笔数';
COMMENT ON COLUMN pay_reconcile_daily.bill_amount IS '渠道账单金额';
COMMENT ON COLUMN pay_reconcile_daily.diff IS '差异明细（本地多、渠道多、金额不一致等）';
COMMENT ON COLUMN pay_reconcile_daily.status IS '对账状态：ok 一致 / diff 有差异 / manual_ok 人工确认无误';

-- ======================================================================
-- 9. 数据与平台域：埋点、汇总、财务、域名
-- ======================================================================

COMMENT ON TABLE event_raw IS '原始埋点事件（docs/02 第 6.3 / 6.3.1 节）：前台与 APP 经 POST /api/v1/events 批量上报，服务端校验后 COPY 入库。按天 RANGE 分区（occurred_at 为 UTC 日界），只保留 occurred_at BRIN 与 device_id 两条索引；汇总查询走 metric_* 预聚合表，90 天后导出 Parquet 并 DETACH/DROP。日分区由周期任务滚动创建，PG 不会把本注释传播到分区';
COMMENT ON COLUMN event_raw.occurred_at IS '事件发生时间（UTC），分区键';
COMMENT ON COLUMN event_raw.event_type IS '事件类型：visitor / play_attempt / play_start / play_error / watch / ad_impression / ad_click / register_wall / register / domain_switch 等。白名单在应用层，不加 CHECK 以便扩展';
COMMENT ON COLUMN event_raw.device_id IS '上报设备标识，用于去重算 UV';
COMMENT ON COLUMN event_raw.user_id IS '注册用户；游客为空';
COMMENT ON COLUMN event_raw.drama_id IS '相关剧集（播放类事件）';
COMMENT ON COLUMN event_raw.episode_id IS '相关分集（播放类事件）';
COMMENT ON COLUMN event_raw.client IS '客户端：web / app 等';
COMMENT ON COLUMN event_raw.region IS '国家地区（GeoIP 解析，可空）';
COMMENT ON COLUMN event_raw.isp IS '运营商（GeoIP 解析，可空）';
COMMENT ON COLUMN event_raw.props IS '事件自有属性（错误码、广告位、观看秒数等）';

COMMENT ON TABLE metric_daily IS '全站日汇总（docs/02 第 6.3 节）：按北京时间日界，凌晨 3 点定稿。看板只读这张表，不扫 event_raw';
COMMENT ON COLUMN metric_daily.day IS '统计日（北京时间）';
COMMENT ON COLUMN metric_daily.uv IS '去重设备数';
COMMENT ON COLUMN metric_daily.play_attempts IS '播放尝试次数';
COMMENT ON COLUMN metric_daily.play_starts IS '播放成功开始次数';
COMMENT ON COLUMN metric_daily.play_errors IS '播放失败次数';
COMMENT ON COLUMN metric_daily.watch_seconds IS '有效观看秒数之和';
COMMENT ON COLUMN metric_daily.registrations IS '注册成功人数';
COMMENT ON COLUMN metric_daily.reg_wall_views IS '注册墙曝光次数';
COMMENT ON COLUMN metric_daily.ad_impressions IS '广告曝光次数';
COMMENT ON COLUMN metric_daily.ad_clicks IS '广告点击次数';
COMMENT ON COLUMN metric_daily.extra IS '预留扩展指标：前台埋点 SDK 接入后的 QoS、留存等先放这里';
COMMENT ON COLUMN metric_daily.finalized_at IS '定稿时间';

COMMENT ON TABLE metric_drama_daily IS '按剧的日汇总（docs/02 第 6.3 节），供剧集分析列表';
COMMENT ON COLUMN metric_drama_daily.day IS '统计日（北京时间）';
COMMENT ON COLUMN metric_drama_daily.drama_id IS '剧集 ID（不建外键，剧删除后历史数据保留）';
COMMENT ON COLUMN metric_drama_daily.viewers IS '去重观看人数';
COMMENT ON COLUMN metric_drama_daily.play_starts IS '播放开始次数';
COMMENT ON COLUMN metric_drama_daily.watch_seconds IS '有效观看秒数之和';
COMMENT ON COLUMN metric_drama_daily.extra IS '预留扩展指标';

COMMENT ON TABLE metric_episode_daily IS '按分集的日汇总（docs/02 第 6.3 节），供单剧的集间留存曲线';
COMMENT ON COLUMN metric_episode_daily.day IS '统计日（北京时间）';
COMMENT ON COLUMN metric_episode_daily.episode_id IS '分集 ID（不建外键，分集删除后历史数据保留）';
COMMENT ON COLUMN metric_episode_daily.drama_id IS '所属剧 ID，便于按剧聚合';
COMMENT ON COLUMN metric_episode_daily.viewers IS '去重观看人数';
COMMENT ON COLUMN metric_episode_daily.play_starts IS '播放开始次数';
COMMENT ON COLUMN metric_episode_daily.watch_seconds IS '有效观看秒数之和';
COMMENT ON COLUMN metric_episode_daily.extra IS '预留扩展指标';

COMMENT ON TABLE metric_hourly IS '小时预聚合（docs/02 第 6.3.1 节）：按 UTC 小时，供看板实时卡片，不扫原始表';
COMMENT ON COLUMN metric_hourly.hour IS '统计小时（UTC 整点）';
COMMENT ON COLUMN metric_hourly.uv IS '该小时去重设备数';
COMMENT ON COLUMN metric_hourly.play_starts IS '该小时播放开始次数';
COMMENT ON COLUMN metric_hourly.watch_seconds IS '该小时有效观看秒数之和';

COMMENT ON TABLE finance_monthly IS '收入与成本（docs/02 第 6.5 节）：运营按月手填收支，供数据看板核算毛利。会员收入不手填，由 pay_order 已支付订单自动汇总。金额一律存人民币分';
COMMENT ON COLUMN finance_monthly.month IS '统计月份，存当月 1 号（北京月）';
COMMENT ON COLUMN finance_monthly.ad_revenue IS '本月广告收入（分）';
COMMENT ON COLUMN finance_monthly.referral_revenue IS '家族站导流 / 互推收入（分）';
COMMENT ON COLUMN finance_monthly.traffic_cost IS '视频流量费（分）';
COMMENT ON COLUMN finance_monthly.server_cost IS '服务器与数据库费（分）';
COMMENT ON COLUMN finance_monthly.payment_fee IS '支付手续费（分）';
COMMENT ON COLUMN finance_monthly.ad_note IS '广告收入备注，不超过 500 字';
COMMENT ON COLUMN finance_monthly.referral_note IS '导流收入备注，不超过 500 字';
COMMENT ON COLUMN finance_monthly.traffic_note IS '流量费备注，不超过 500 字';
COMMENT ON COLUMN finance_monthly.server_note IS '服务器费备注，不超过 500 字';
COMMENT ON COLUMN finance_monthly.payment_note IS '手续费备注，不超过 500 字';
COMMENT ON COLUMN finance_monthly.updated_at IS '最近填写时间';

COMMENT ON TABLE domain_entry IS '入口域名池（docs/02 第 6.4 节）。证书不入库：入口证书由部署层 ACME 自动签发，后台只维护到期日并在到期前告警';
COMMENT ON COLUMN domain_entry.id IS '域名记录 ID';
COMMENT ON COLUMN domain_entry.host IS '域名，全站唯一，1–253 字';
COMMENT ON COLUMN domain_entry.role IS '用途：web_primary Web 主入口 / web_backup 备用入口 / nav 永久导航 / api API 域名 / media 媒体域名 / admin 后台';
COMMENT ON COLUMN domain_entry.weight IS '同角色内的权重（1–100）';
COMMENT ON COLUMN domain_entry.region IS '适用地区：ISO 3166-1 alpha-2 代码，* 表示全部地区';
COMMENT ON COLUMN domain_entry.cdn IS '所属 CDN 厂商，便于排障';
COMMENT ON COLUMN domain_entry.dns_provider IS 'DNS 服务商';
COMMENT ON COLUMN domain_entry.cert_expires_at IS '证书到期日；未录入或未上传过证书时为空';
COMMENT ON COLUMN domain_entry.status IS '状态：active 启用 / watch 观察（仍在清单里但排在后面）/ disabled 停用';
COMMENT ON COLUMN domain_entry.note IS '运维备注，不超过 200 字';
COMMENT ON COLUMN domain_entry.created_at IS '创建时间';
COMMENT ON COLUMN domain_entry.updated_at IS '最近修改时间';

COMMENT ON TABLE domain_list_release IS '签名域名清单的发布历史（docs/09 第 4.1 节）。每次发布（含回滚历史版本的内容）都是一个更大的新版本';
COMMENT ON COLUMN domain_list_release.version IS '清单版本号，即清单里的 v。由应用层取「当前最大版本 + 1」显式赋值（不用自增）：v 被写进被签名的 payload，发布时要校验两者一致';
COMMENT ON COLUMN domain_list_release.payload IS '被签名的清单正文（含 v，不含 sig）';
COMMENT ON COLUMN domain_list_release.signature IS '离线签发工具（service/cmd/keytool）签出的 Ed25519 签名的 base64；后台不保存签名私钥';
COMMENT ON COLUMN domain_list_release.note IS '发布备注，不超过 200 字';
COMMENT ON COLUMN domain_list_release.published_by IS '发布者；账号删除后置空而非级联删除（清单比账号活得久）';
COMMENT ON COLUMN domain_list_release.published_at IS '发布时间';

COMMENT ON TABLE domain_health_daily IS '域名健康日表（docs/02 第 6.4 节）。probe_* 来自服务端拨测，client_* 与 fail_reasons 来自客户端切换上报';
COMMENT ON COLUMN domain_health_daily.day IS '统计日';
COMMENT ON COLUMN domain_health_daily.host IS '域名';
COMMENT ON COLUMN domain_health_daily.region IS '探测来源地区，* 表示不区分';
COMMENT ON COLUMN domain_health_daily.probe_ok IS '服务端拨测成功次数';
COMMENT ON COLUMN domain_health_daily.probe_fail IS '服务端拨测失败次数';
COMMENT ON COLUMN domain_health_daily.client_failover_count IS '客户端上报的切换次数';
COMMENT ON COLUMN domain_health_daily.fail_reasons IS '失败原因分布：{"dns":..,"tls":..,"timeout":..,"http5xx":..}';

-- ======================================================================
-- 10. 反馈社区（feedback/docs/03）：与短剧业务同库不同表，统一 fb_ 前缀
-- fb_user 与 admin_user 是两套身份：运营靠后台 RBAC 管理社区，不需要社区账号
-- ======================================================================

COMMENT ON TABLE fb_user IS '反馈社区用户。账号只有用户名 + 密码，不收集邮箱与手机号；无找回密码手段（feedback/docs/adr/0011）';
COMMENT ON COLUMN fb_user.id IS '社区用户 ID';
COMMENT ON COLUMN fb_user.handle IS '用户名（保留原始大小写），3–32 位 [A-Za-z0-9_-]';
COMMENT ON COLUMN fb_user.handle_norm IS 'handle 的小写形式，唯一；用来实现大小写不敏感的唯一性';
COMMENT ON COLUMN fb_user.password_hash IS '密码哈希（Argon2id PHC 串）';
COMMENT ON COLUMN fb_user.recovery_code_hash IS '已废弃（adr/0011）：恢复码功能已删除，此列恒为空串，不再读写。刻意不 DROP COLUMN，以便整体回滚且不改动 sqlc 生成的字段类型';
COMMENT ON COLUMN fb_user.nickname IS '展示昵称，不超过 40 字';
COMMENT ON COLUMN fb_user.bio IS '个人简介，不超过 300 字';
COMMENT ON COLUMN fb_user.show_level IS '是否公开贡献等级与署名';
COMMENT ON COLUMN fb_user.status IS '状态：active 正常 / restricted 受限 / suspended 封禁';
COMMENT ON COLUMN fb_user.session_version IS '会话版本：改密、重置、封禁时 +1，旧会话立即失效';
COMMENT ON COLUMN fb_user.points IS '贡献值缓存，可由 fb_contribution 重算';
COMMENT ON COLUMN fb_user.tester_no IS '内测编号（从 1001 起），展示为「内测1001」；一旦分配不再变化，注销也不回收';
COMMENT ON COLUMN fb_user.created_at IS '注册时间';
COMMENT ON COLUMN fb_user.updated_at IS '最近修改时间';

COMMENT ON TABLE fb_project IS '内测项目。只有平台管理员能创建（feedback/docs/adr/0001）';
COMMENT ON COLUMN fb_project.id IS '项目 ID';
COMMENT ON COLUMN fb_project.slug IS '项目别名，全站唯一，小写短横线格式';
COMMENT ON COLUMN fb_project.key_prefix IS '反馈编号前缀（大写字母开头，2–8 位），唯一；反馈展示为 APP-1024 这种形式';
COMMENT ON COLUMN fb_project.name IS '项目名称，1–60 字';
COMMENT ON COLUMN fb_project.summary IS '一句话简介，不超过 200 字';
COMMENT ON COLUMN fb_project.description IS '项目详情，不超过 10000 字';
COMMENT ON COLUMN fb_project.cover IS '项目封面相对 key';
COMMENT ON COLUMN fb_project.try_url IS '「去试用」地址，只允许 https';
COMMENT ON COLUMN fb_project.status IS '状态：draft 草稿 / recruiting 招募中 / active 进行中 / paused 暂停 / archived 已归档';
COMMENT ON COLUMN fb_project.visibility IS '可见范围：public 公开 / members 仅成员';
COMMENT ON COLUMN fb_project.join_mode IS '加入方式：open 自由加入 / application 需申请 / invite_only 仅邀请 / closed 已关闭';
COMMENT ON COLUMN fb_project.next_number IS '项目内反馈编号计数器，在事务内分配，保证编号连续';
COMMENT ON COLUMN fb_project.sort IS '列表排序';
COMMENT ON COLUMN fb_project.created_at IS '创建时间';
COMMENT ON COLUMN fb_project.updated_at IS '最近修改时间';

COMMENT ON TABLE fb_project_member IS '项目成员。owner / maintainer / operator 属于项目团队，member 是测试成员';
COMMENT ON COLUMN fb_project_member.project_id IS '项目；项目删除时级联删除';
COMMENT ON COLUMN fb_project_member.user_id IS '社区用户；用户删除时级联删除';
COMMENT ON COLUMN fb_project_member.role IS '角色：member 测试成员 / operator 运营 / maintainer 维护者 / owner 拥有者';
COMMENT ON COLUMN fb_project_member.status IS '状态：pending 待通过 / active 正常 / removed 已移除';
COMMENT ON COLUMN fb_project_member.joined_at IS '加入时间';

COMMENT ON TABLE fb_invite IS '邀请码。明文只在创建时返回一次，库里只存哈希';
COMMENT ON COLUMN fb_invite.id IS '邀请码 ID';
COMMENT ON COLUMN fb_invite.code_hash IS '邀请码哈希，唯一；明文不入库';
COMMENT ON COLUMN fb_invite.code_hint IS '邀请码前 4 位，供运营辨认';
COMMENT ON COLUMN fb_invite.project_id IS '自动加入的项目；为空表示只注册、不自动加入';
COMMENT ON COLUMN fb_invite.role IS '核销后获得的项目角色：member / operator / maintainer';
COMMENT ON COLUMN fb_invite.max_uses IS '最多可核销次数（1–10000）';
COMMENT ON COLUMN fb_invite.used_count IS '已核销次数，不得超过 max_uses（CHECK 约束）';
COMMENT ON COLUMN fb_invite.note IS '运营备注，不超过 100 字';
COMMENT ON COLUMN fb_invite.expires_at IS '过期时间；为空表示不过期';
COMMENT ON COLUMN fb_invite.revoked_at IS '作废时间；非空表示已失效';
COMMENT ON COLUMN fb_invite.created_by IS '签发的后台管理员；有邀请码的管理员不能删除（ON DELETE RESTRICT）';
COMMENT ON COLUMN fb_invite.created_at IS '创建时间';

COMMENT ON TABLE fb_invite_redemption IS '邀请核销记录：一个用户对同一邀请只能核销一次';
COMMENT ON COLUMN fb_invite_redemption.invite_id IS '邀请码；删除时级联删除';
COMMENT ON COLUMN fb_invite_redemption.user_id IS '核销用户；用户删除时级联删除';
COMMENT ON COLUMN fb_invite_redemption.created_at IS '核销时间';

COMMENT ON TABLE fb_post IS '项目动态：公告、测试任务、上线小结。只有项目团队能发（成员不能发讨论帖）';
COMMENT ON COLUMN fb_post.id IS '动态 ID';
COMMENT ON COLUMN fb_post.project_id IS '所属项目；项目删除时级联删除';
COMMENT ON COLUMN fb_post.kind IS '类型：announcement 公告 / test_task 测试任务 / changelog 上线小结';
COMMENT ON COLUMN fb_post.title IS '标题，1–120 字';
COMMENT ON COLUMN fb_post.body IS '正文：受限 Markdown 源文，不超过 50000 字';
COMMENT ON COLUMN fb_post.status IS '状态：draft 草稿 / published 已发布 / hidden 已隐藏';
COMMENT ON COLUMN fb_post.visibility IS '可见范围：public 公开 / members 仅成员';
COMMENT ON COLUMN fb_post.pinned IS '是否置顶';
COMMENT ON COLUMN fb_post.published_at IS '发布时间；status=published 时必须非空（CHECK 约束）';
COMMENT ON COLUMN fb_post.created_at IS '创建时间';
COMMENT ON COLUMN fb_post.updated_at IS '最近修改时间';

COMMENT ON TABLE fb_feedback IS '反馈。number 在项目内连续，展示为 <key_prefix>-<number>';
COMMENT ON COLUMN fb_feedback.id IS '反馈 ID（内部）';
COMMENT ON COLUMN fb_feedback.project_id IS '所属项目；项目删除时级联删除';
COMMENT ON COLUMN fb_feedback.number IS '项目内编号，与 project_id 组成唯一约束';
COMMENT ON COLUMN fb_feedback.author_id IS '提交的社区用户；注销后置空，保留反馈与状态历史';
COMMENT ON COLUMN fb_feedback.kind IS '类型：bug 缺陷 / experience 体验 / feature 需求 / content 内容 / access 访问 / competitor 竞品发现 / other 其他';
COMMENT ON COLUMN fb_feedback.title IS '标题，4–160 字';
COMMENT ON COLUMN fb_feedback.body IS '正文，1–20000 字';
COMMENT ON COLUMN fb_feedback.env IS '环境上下文（机型、浏览器、网络等），不超过 500 字，不含精确位置';
COMMENT ON COLUMN fb_feedback.visibility IS '可见范围：public 公开 / members 仅成员 / private 仅本人与团队';
COMMENT ON COLUMN fb_feedback.status IS '状态：submitted 待分诊 / triaged 已分诊 / planned 已排期 / in_progress 处理中 / verifying 待验证 / shipped 已上线 / closed 已关闭 / duplicate 重复 / declined 不做';
COMMENT ON COLUMN fb_feedback.priority IS '优先级：critical / high / medium / low / unset 未定';
COMMENT ON COLUMN fb_feedback.duplicate_of IS '重复指向的主反馈；status=duplicate 时必须非空，其他状态必须为空（CHECK 约束）';
COMMENT ON COLUMN fb_feedback.vote_count IS '赞同票数（fb_vote 的缓存）';
COMMENT ON COLUMN fb_feedback.version IS '乐观锁版本号，后台保存时比对，防并发覆盖';
COMMENT ON COLUMN fb_feedback.source IS '来源：community 社区用户自己提交 / open_api 外部网站经 API Key 送入';
COMMENT ON COLUMN fb_feedback.api_key_id IS '送入本条的开放接口 Key；source=open_api 时必须非空（CHECK 约束）';
COMMENT ON COLUMN fb_feedback.contact IS '提交者自愿留的联系方式（外部提交无账号时回访用），不超过 120 字；不做校验、不外发';
COMMENT ON COLUMN fb_feedback.page_url IS '出问题的页面地址，只存站内路径或脱敏后的地址，不超过 500 字';
COMMENT ON COLUMN fb_feedback.last_activity_at IS '最近活动时间，列表按它倒序';
COMMENT ON COLUMN fb_feedback.created_at IS '提交时间';
COMMENT ON COLUMN fb_feedback.updated_at IS '最近修改时间';

COMMENT ON TABLE fb_feedback_event IS '反馈状态历史：只追加，不更新';
COMMENT ON COLUMN fb_feedback_event.id IS '记录 ID';
COMMENT ON COLUMN fb_feedback_event.feedback_id IS '所属反馈；反馈删除时级联删除';
COMMENT ON COLUMN fb_feedback_event.from_status IS '变更前状态；首条为空串';
COMMENT ON COLUMN fb_feedback_event.to_status IS '变更后状态';
COMMENT ON COLUMN fb_feedback_event.note IS '公开说明，不超过 1000 字，提交者可见';
COMMENT ON COLUMN fb_feedback_event.internal_note IS '内部备注，不超过 1000 字，只给后台';
COMMENT ON COLUMN fb_feedback_event.actor_admin IS '操作的后台管理员；账号删除时置空';
COMMENT ON COLUMN fb_feedback_event.created_at IS '发生时间';

COMMENT ON TABLE fb_vote IS '反馈投票：一个用户对一条反馈只能一票';
COMMENT ON COLUMN fb_vote.feedback_id IS '反馈；删除时级联删除';
COMMENT ON COLUMN fb_vote.user_id IS '投票用户；用户删除时级联删除';
COMMENT ON COLUMN fb_vote.created_at IS '投票时间';

COMMENT ON TABLE fb_contribution IS '贡献事件：只追加，撤销写反向分（feedback/docs/adr/0005，非货币化）';
COMMENT ON COLUMN fb_contribution.id IS '记录 ID';
COMMENT ON COLUMN fb_contribution.user_id IS '获得贡献的用户；用户删除时级联删除';
COMMENT ON COLUMN fb_contribution.project_id IS '相关项目；项目删除时置空';
COMMENT ON COLUMN fb_contribution.feedback_id IS '相关反馈；反馈删除时置空';
COMMENT ON COLUMN fb_contribution.kind IS '类型：triaged 被分诊 / shipped 被采纳上线 / helped 回答被认可 / topic 参与议题 / manual 人工调整 / reverse 撤销反向分';
COMMENT ON COLUMN fb_contribution.points IS '本次分值，可为负（reverse）';
COMMENT ON COLUMN fb_contribution.reason IS '说明，不超过 200 字';
COMMENT ON COLUMN fb_contribution.source_key IS '自动事件的幂等键（如 shipped:123），唯一，防重复发分';
COMMENT ON COLUMN fb_contribution.created_at IS '发生时间';

COMMENT ON TABLE fb_api_key IS '开放提交接口的 API Key：其他网站把用户反馈送进来，运营后台统一分诊。明文只在创建时返回一次';
COMMENT ON COLUMN fb_api_key.id IS 'Key 记录 ID';
COMMENT ON COLUMN fb_api_key.project_id IS '送入的目标项目；项目删除时级联删除';
COMMENT ON COLUMN fb_api_key.name IS '用途说明，1–60 字';
COMMENT ON COLUMN fb_api_key.key_hash IS '完整密钥的哈希，唯一；明文不入库';
COMMENT ON COLUMN fb_api_key.key_hint IS '密钥开头几位，供后台识别';
COMMENT ON COLUMN fb_api_key.default_visibility IS '送入反馈的默认可见范围，默认 members：外部提交先进后台分诊，由运营决定是否转公开';
COMMENT ON COLUMN fb_api_key.allowed_origins IS '允许的浏览器 Origin；空数组表示不校验来源';
COMMENT ON COLUMN fb_api_key.daily_limit IS '每日提交上限（1–100000），默认 500';
COMMENT ON COLUMN fb_api_key.status IS '状态：active 启用 / inactive 停用';
COMMENT ON COLUMN fb_api_key.used_count IS '累计提交次数';
COMMENT ON COLUMN fb_api_key.last_used_at IS '最近使用时间';
COMMENT ON COLUMN fb_api_key.revoked_at IS '作废时间；非空表示已失效';
COMMENT ON COLUMN fb_api_key.created_by IS '签发的后台管理员；有 Key 的管理员不能删除（ON DELETE RESTRICT）';
COMMENT ON COLUMN fb_api_key.created_at IS '创建时间';
COMMENT ON COLUMN fb_api_key.updated_at IS '最近修改时间';

COMMENT ON TABLE fb_topic IS '议题：官方抛运营向问题，用户自由回答（feedback/docs/01 第 11 节）。首期只做自由回答，单选 / 多选议题等确认后再加';
COMMENT ON COLUMN fb_topic.id IS '议题 ID';
COMMENT ON COLUMN fb_topic.project_id IS '所属项目；项目删除时级联删除';
COMMENT ON COLUMN fb_topic.title IS '议题标题，1–160 字';
COMMENT ON COLUMN fb_topic.body IS '议题正文，不超过 20000 字';
COMMENT ON COLUMN fb_topic.answer_hint IS '回答框的引导语，不超过 200 字';
COMMENT ON COLUMN fb_topic.status IS '状态：draft 草稿 / open 征集中 / closed 已结束 / archived 已归档';
COMMENT ON COLUMN fb_topic.visibility IS '可见范围：public 公开 / members 仅成员';
COMMENT ON COLUMN fb_topic.pinned IS '是否置顶';
COMMENT ON COLUMN fb_topic.answer_count IS '回答数（fb_topic_answer 的缓存）';
COMMENT ON COLUMN fb_topic.ends_at IS '征集截止时间；为空表示不限';
COMMENT ON COLUMN fb_topic.published_at IS '发布时间；非草稿时必须非空（CHECK 约束）';
COMMENT ON COLUMN fb_topic.created_by IS '创建的后台管理员；账号删除时置空';
COMMENT ON COLUMN fb_topic.created_at IS '创建时间';
COMMENT ON COLUMN fb_topic.updated_at IS '最近修改时间';

COMMENT ON TABLE fb_topic_answer IS '议题回答。只允许一层回复：回复的回复挂到同一个顶层回答下';
COMMENT ON COLUMN fb_topic_answer.id IS '回答 ID';
COMMENT ON COLUMN fb_topic_answer.topic_id IS '所属议题；议题删除时级联删除';
COMMENT ON COLUMN fb_topic_answer.parent_id IS '被回复的顶层回答；为空表示本身是顶层回答';
COMMENT ON COLUMN fb_topic_answer.user_id IS '回答用户；注销后置空，保留内容';
COMMENT ON COLUMN fb_topic_answer.body IS '回答正文，1–5000 字';
COMMENT ON COLUMN fb_topic_answer.featured IS '是否运营标记的「精选」，列表优先展示';
COMMENT ON COLUMN fb_topic_answer.helpful_count IS '「有道理」计数（fb_answer_helpful 的缓存）';
COMMENT ON COLUMN fb_topic_answer.status IS '状态：visible 显示 / hidden 隐藏';
COMMENT ON COLUMN fb_topic_answer.created_at IS '发表时间';
COMMENT ON COLUMN fb_topic_answer.updated_at IS '最近修改时间';

COMMENT ON TABLE fb_answer_helpful IS '「有道理」：一个用户对一条回答只能点一次';
COMMENT ON COLUMN fb_answer_helpful.answer_id IS '回答；删除时级联删除';
COMMENT ON COLUMN fb_answer_helpful.user_id IS '点击用户；用户删除时级联删除';
COMMENT ON COLUMN fb_answer_helpful.created_at IS '点击时间';

-- +goose Down
-- 清空本迁移覆盖范围内的全部表与列注释，再恢复 00031 对 fb_user.recovery_code_hash 的废弃说明
-- （那条注释属于 00031，不应随本迁移回滚一起消失）。
-- 逐条写近千句 COMMENT ON ... IS NULL 没有意义，用一次目录扫描完成；
-- 排除条件与 Up 的覆盖范围一致：不碰 goose 与 River 自建表，也不碰 event_raw 的日分区。
-- +goose StatementBegin
DO $$
DECLARE
    tbl record;
    col record;
BEGIN
    FOR tbl IN
        SELECT c.oid, c.relname
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public'
          AND c.relkind IN ('r', 'p')
          AND c.relname <> 'goose_db_version'
          AND c.relname NOT LIKE 'river\_%'
          AND c.relname NOT LIKE 'event\_raw\_%'
    LOOP
        EXECUTE format('COMMENT ON TABLE public.%I IS NULL', tbl.relname);
        FOR col IN
            SELECT a.attname
            FROM pg_attribute a
            WHERE a.attrelid = tbl.oid AND a.attnum > 0 AND NOT a.attisdropped
        LOOP
            EXECUTE format('COMMENT ON COLUMN public.%I.%I IS NULL', tbl.relname, col.attname);
        END LOOP;
    END LOOP;
END $$;
-- +goose StatementEnd

COMMENT ON COLUMN fb_user.recovery_code_hash IS
    '已废弃（adr/0011）：恢复码功能已删除，此列恒为空串，不再读写。';
