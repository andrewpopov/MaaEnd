# 战斗参考资料库

资料读取日期为 2026-10-17（America/Los_Angeles）。本地资料库包含来自 13 篇 Prydwen 角色指南的 34 个不同队伍和 18 个已发布的循环示例，覆盖 Heat、Cryo、Electric、Physical 和 Nature 队伍。配装前提及后续更新以原始指南为准。

## 文件

- `catalog.json`：队伍成员、来源链接、动作顺序、前置条件，以及包含 168 个事件的四个时间轴示例。
- `roster_resume.json`：已暂存的角色扫描断点和模拟器恢复条件。
- `../Capture-Roster.ps1` 和 `../capture_roster.py`：截图启动器和实现。

## 循环覆盖范围

十二个队伍有明确的循环示例。其他队伍仅保存阵容和来源供比较，不套用不同阵容的循环。起手和准备片段与场景循环分别标注。所有示例均尚未根据本账号完整的角色、装备及实战进行验证。

已发布的示例包括满能量爆发、零能量起手、Wulfgard P5 分支、Crush/Breach 分支，以及 Arcane、Mi Fu、Typhoeus 和 Zhuang Fangyi 的时间轴。有些指南列出了替代阵容，但未提供完整循环。已检查的 Si 指南队伍区域缺少第四个头像，因此只记录到待复核列表，不推测缺失位置。

## 时机与执行

动作保留角色名称。角色为 null 表示来源未明确当前操作角色。多次 Final Strike 表示完成普攻连段，而非单次普攻点击。执行时须满足连携可用、SP、附着层数、目标状态、切人、潜能及装备效果等条件。Rossi 连携的第二次输入需要单独校验时机。

时间轴秒数来自页面显示的坐标轴和事件位置，仅表示来源示意图位置，不是测量得到的输入时间。同一位置的事件没有验证过的串行输入顺序。这些文件是参考数据，不是可执行的 EndAxis 数据码或已经启用的 MaaEnd 战斗配置。现有导入器只覆盖战技和终结技，其余动作仍需实现并进行实战验证。

## 暂存的扫描

较早的 PC 归档保存六个角色、48 张截图，目录记录在 `roster_resume.json` 中；它仍是部分历史证据。自动 UI 任务使用已验证的 Endies 模拟器英文 1280x720 界面，并根据实际读取的 UID 绑定独立的运行时会话。不要操作 Arkies/Arknights。

使用 MaaEnd 中的四个核心账号扫描任务，采集当前干员、技能、武器和防具证据。`../Capture-Roster.ps1` 保留为较早的截图辅助工具，不驱动这些新批量流程。选择队伍或消耗制造、升级材料前，先核对采集的账号数据。

## 全角色配装

`characters.json` 和 `characters/` 保存全部 34 个目录条目的配装资料：33 个已有配装的角色，以及尚未发布配装的 Argent Flow。资料保留了 99 条武器推荐及其武器潜能、54 套完整的四槽装备，以及 36 条按定位区分的技能投资顺序。`guides/INDEX.txt` 提供各角色原创摘要和来源指南链接。队伍和循环 ID 对应 `catalog.json`。

本快照保留普通或特殊武器章节各前三条推荐，以及每篇指南前两套完整配装。更多替代方案、精确属性阈值、计算和详细战斗说明以来源指南为准。不推测独立的属性优先级，也未将任何配装应用到账号。

## 账号扫描任务与存储

`account_scan_tasks.json` 定义十二项只读任务：账号身份、角色、技能与天赋、已装备武器与基质、武器库存、已装备防具、装备库存、已保存配装、制造配方、制造材料与货币、精锻，以及编队预设。每项任务列出必需字段、证据界面、依赖项和导航验证状态。`account_scan_resume.json` 指向当前会话，Si 计划也引用该会话。

数据库位于 `install/data/AccountScans/account_scans.sqlite3`，原始截图保存在 `screenshots/`，可移植的会话快照保存在 `exports/`。账号标签与已验证的 UID 分开保存。导入之前的角色截图时保留原始截图时间。该存档提供 48 张部分证据截图，目前没有已解析或已审核的账号记录。

在仓库根目录运行以下命令，会话 ID 取自 `account_scan_resume.json`：

```powershell
.\tools\Scan-Account.ps1 status --session <session-id>
.\tools\Scan-Account.ps1 export --session <session-id>
.\tools\Scan-Account.ps1 capture --session <session-id> --task gear_recipes --entity recipe-name --page cost-preview --instance <instance-id>
.\tools\Scan-Account.ps1 import-image --session <session-id> --task gear_recipes --entity recipe-name --page cost-preview --input screenshot.png
```

`capture` 仅通过 MaaEnd 截图订阅保存已打开的界面，审核时须检查内容和时效。该命令不负责跳转至配方，也不进行制造、装备、升级或精锻。未映射界面的自动导航须在 Endies 上验证后才能完成；之前的角色导航也尚未通过模拟器验证。

使用 `record --input record.json --evidence <evidence-id> ...` 为实体关联解析后的 JSON。逐项对照截图确认必需字段后才能添加 `--reviewed`。未知数量保留为 null，不得将库存预览推定为已装备。证据不可跨账号、任务或实体使用。完成任务使用 `complete --coverage-note "..."`，要求依赖项和记录均已审核，并覆盖所有已捕获界面。新增证据会撤销相应审核和下游任务的完成状态。记录和导出文件仅保存在本地，不上传账号数据。

## 制作、装备与升级路径

`../gear_workflow.py` 根据账号存储生成离线预览。`plans/si-gear-request.json` 指定 Si 的四件候选装备；`install/data/GearPlans/si-preview.json` 保存当前缺失数据报告。规划器不发送游戏输入。制作、装备验证、保存并重新打开配装、单次精锻已有步骤约定；模拟器导航、武器等级/突破和技能升级仍待实现。

```powershell
.\.venv\Scripts\python.exe tools/gear_workflow.py --session 7ea55097fa8a4daa80c52100629d39f5 --request tools/combat_reference/plans/si-gear-request.json --output install/data/GearPlans/si-preview.json
```

请求包含 `operator`、`loadout_name`、四个 `gear` 槽位（`armor`、`gloves`、`kit_1`、`kit_2`）、`spend_limits`、`donor_allowlist` 和 `artifice_attempts`。各装备目标包含 `item_name`，并可指定准确的 `item_id` 或 `recipe_id`。已有同名配装需要显式指定其 `overwrite_plan_id`。先复用已有装备，再考虑制作；存在多个副本时必须指定。每个库存实体副本需要唯一的 `item_id` 和 `quantity: 1`；库存配件记录使用 `slot: "kit"`。其他干员正在装备的物品不参与选择。配方批量产出的剩余副本会先被预留，再考虑再次制作。

配方的 `materials` 和 `currency_cost` 均须为资源名称到整数的显式对象，已确认零成本时填写 `{}`。必须明确 `batch_size`、`recipe_id`、`technique`、槽位和解锁状态。材料/货币余额名称必须唯一，数量必须已知。制作与精锻共用资源总额，且必须同时满足库存余额和显式 `spend_limits`；缺少成本或限额会阻止预览就绪。

每个精锻请求指定 `target_item_id`、`donor_item_id` 和 `stat_line`。匹配且已审核的 `artificing` 记录提供当前/最高等级、保底状态、匹配质量、0 到 1 之间的 `success_chance`，以及显式 `catalyst_cost`/`other_costs` 对象。素材装备必须明确获准使用、未装备、未锁定、未收藏、未强化且槽位兼容。最终装备不能作为素材。每个目标只规划一次尝试；再次尝试前必须重新扫描结果与实际消耗。固定次数不能保证词条等级。

账号身份和相关扫描覆盖率必须已审核，关联记录不得早于 24 小时，且存储证据校验和必须一致。每份预览包含输入指纹，并保持 `execution_enabled: false`。未来的执行器必须在每次操作前验证当前账号、实时前提、成本和物品 ID，根据已观察的库存变化绑定新制作副本，并在结果不确定时停止，避免重复消耗。Si 当前的预览被阻止，因为截图尚未审核为账号记录。

## 手动流程录制

`../record_workflow.py` 通过 MaaEnd 本地截图 API 录制已连接的原生控制器。传入 `--api-url http://127.0.0.1:<port>/api`、`--instance <instance-id>` 和新的 `--output <directory>`。它仅发送截图订阅/退订请求和截图读取请求；不记录点击、按键或音频，也不启动游戏任务。原始 PNG 按 SHA-256 去重，`frames.jsonl` 保留每次采样的时间戳。OCR 处理仍是独立步骤。

由于 MXU 会在 30 秒后清理订阅者，录制器每 10 秒续订一次。`status.json` 包含最近变化时间，并标记连续 15 秒未变化的画面；画面不变也可能是场景静止。在输出目录创建 `STOP` 即可结束录制。录制也会在达到 30 分钟、唯一 PNG 总量达到 1 GiB 或连续三次截图错误后停止。第一次客栈录制未续订，因此其前 30 秒之后的帧无法证明后续流程。

## 离线 OCR 审核

`../review_workflow_ocr.py <recording-directory>` 使用已安装的 MaaEnd OCR 模型处理每张唯一的归档 PNG。它仅绑定资源，不绑定实时控制器。可恢复处理的 `ocr/results.jsonl` 保留完整 OCR 结果、边界框和置信度；`ocr/transcript.txt` 和 `ocr/timeline.json` 包含置信度至少为 0.8 的文本，并省略 UID 行。原始 OCR 保存在本地，可能包含标识信息。`ocr/summary.json` 记录覆盖率和模型哈希。随后运行 `../review_inn_signals.py <recording-directory>` 生成 `ocr/inn-review.json`，保存客栈状态、队列警告、顾客排除条件、协助收账和会话结束的时间戳证据。可选的 `ocr/visual-review.json` 人工注释与 OCR 观察结果分开保留。

截图时间是采样时间，不是输入事件。OCR 无法识别未标注名称的菜品图标、重建玩家输入、证明导航路线或确立最优策略。请对照原始 PNG 审核重要结果，排除初始缓存旧图和过渡画面，未来的客栈自动化应结合模板/图标识别与 OCR。

## 页面读取与客栈动作状态

四个核心账号任务现已加入 Endies/ADB 自动流程：已拥有干员的 Overview、能力矩阵的全部四项技能详情与可见天赋节点、已装备武器/基质/潜能，以及四个已装备防具槽位。流程先将干员列表滚到顶部，再逐个访问干员，核对页面标题，并保存按名称归档的截图、OCR 文本与边界框。列表和装备详情滚动均有上限，必须确认末端已静止。矩阵中的各节点根据可见图案对齐；Endministrator 使用已观察到的八节点树，而非常规的十二节点树。详情采集会等待实际分类标题出现。原生控制器保留当前页面读取模式；其他库存、配方、材料和队伍任务仍需先打开对应页面。

批量采集根据实际读取的 UID 区分账号。`ui_session.json` 指向 SQLite 会话和可读的 `exports/<session-id>-ocr.txt`；JSON 导出保留各页面证据与字段草稿。只有每位已访问干员的预期页面齐全，且详情流程的干员名称与近期已拥有干员采集结果一致，才标记为 `captured`；页面缺失或无法读取时任务失败并保留部分结果。采集数据仍未审核：不会验证库存，也不会启用制造、升级或换装。已拥有干员流程于 2026-10-18 通过了 29 位干员的实时遍历验证。能力与装备流程仍在验证全部干员；各任务的导航状态以任务规格为准。其余页面读取任务在 UI 中标为 [Manual page]。

`Innkeeping: read next action` 是有界的只读 UI 动作。它分类已录制的英文客栈状态，并在 `data/Innkeeping` 保存一份报告；不会开始营业、烹饪、上菜或移动。独立规划器按顺序优先处理手持且符合要求的菜品、顾客需要的已完成菜品、尚未被覆盖的已确认烹饪需求和未收账单。它只预留一次队列产出，比较已观察到的厨师专属烹饪时间、移动时间和剩余窗口，遵守满队列及协助收账状态，并避免预测性烹饪。菜品要求和库存必须是已验证输入；实时 OCR 读取会明确报告缺少图标映射，而不是猜测。自动选菜、移动和输入/结果验证尚未实现。回放测试覆盖全部 564 张录制画面；这是识别检查，不是实时自动经营验证。
