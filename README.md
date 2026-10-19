# sonolus-notgarupa-particle-generator

把原作的判定粒子 prefab（tap effect 与方向键特效，Unity ParticleSystem 参数）以及通关 / Full Combo / All Perfect 烟花转换为 Sonolus 粒子包。默认运行即生成 NotGarupa 需要的**全部**粒子条目，没有按条目选择的参数。每个 prefab 输出一个面向屏幕的效果，以及每个 lane 平面倾角一个 `<prefab>#plane<角度>` 效果（四边形纵向按 cos 压缩）。

## 条目

条目由 `internal/master`（原作 JP 10.1.4 的皮肤主数据）推出，共 154 个：

| 组成 | 来源 | 数量 |
|---|---|---|
| 普通设置 | tap effect 5 × 方向键皮肤 5 × 方向键特效设置 normal / light | 50 |
| 特殊组合（Collaboration / Limited） | 组合中的 tap effect（persona、miku、cafe、maid、coin、witch、stage、delta、april2019）× 其余轴的普通设置；persona 的方向键组合另固定方向键皮肤 | 92 |
| `#HABAHIRO` | habahiro tap effect（含宽度变体与换道闪光所在 bundle）× 全部可选方向键皮肤（5 个普通 + persona）× normal / light | 12 |

条目名 `notgarupa-<tap effect>-<方向键皮肤><normal|light>`，例如 `notgarupa-skin00-skin00normal`（默认）、`notgarupa-skin_witch-skin03light`、`notgarupa-habahiro-skin00normal`（HABAHIRO 关卡默认）。HABAHIRO 条目的 `Result.Habahiro` 为 true。它们不是与普通条目并列的粒子：服务器不在列表中提供它们，只按玩家的方向键配置项为 `#HABAHIRO` 关卡选用对应条目（见 `sonolus-notgarupa-server` 的 README）。

主数据中无法取得素材的组合（collaboration 3 / seasonSpecialId 36）整组排除，见 `internal/master`；任何其余素材下载失败都是错误，不会生成残缺的条目。

## 命令行

从 GitHub Release 下载对应平台的压缩包（linux / windows / darwin，amd64 / arm64；由 `.github/workflows/build.yml` 矩阵编译，推送 `v*` 标签时发布），解压后直接运行；也可以用 Go 安装或在本仓库中运行（仓库为私有，需要 `GOPRIVATE=github.com/WindowsSov8forUs/*` 和可访问仓库的 git 凭据）：

```sh
go install github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/cmd/sonolus-notgarupa-particle-generator@latest
go run ./cmd/sonolus-notgarupa-particle-generator   # 在本仓库中
```

```sh
sonolus-notgarupa-particle-generator [-out out] [-cache .cache] [-offline] [-v] [-j N] [-asset-version <CDN 资源版本>] [-version]
```

| 参数 | 默认值 | 作用 |
|---|---|---|
| `-out` | `out` | 输出目录：`<out>/source`（sonolus-pack 源）、`<out>/pack`（可直接加载的包，总是打包）、`<out>/contracts/<条目>.json`（每个条目的效果契约） |
| `-cache` | `.cache` | 下载缓存目录（CDN bundle 在 `<cache>/cdn/<版本>/`） |
| `-offline` | false | 只使用缓存中的下载 |
| `-v` | false | 输出下载日志与每个条目 |
| `-j` | CPU 数 | 并行转换数 |
| `-asset-version` | 固定版本 | 官方 CDN 资源版本（只在验证新版本时使用） |
| `-version` | — | 输出版本号后退出 |

全量运行约 2 分钟（8 核），`out/pack` 约 48 MB（各条目相同的资源文件按哈希只存一份），源目录约 124 MB。

## 素材来源

- 判定粒子：从原作官方资源 CDN（`https://content.garupa.jp/Release/<版本>/Android/<bundle>`）下载 `ingameskin/tapeffect/<tap effect>` 与 `ingameskin/tapeffect/directionalflick<方向键皮肤><normal|light>`，资源版本固定为 `internal/cdn` 的 `PinnedVersion`（`10.1.0.200_e019f5bb…`）。程序直接解析 UnityFS（LZ4）与带 type tree 的序列化文件，读取 GameObject / Transform / ParticleSystem / ParticleSystemRenderer / Material / Mesh，按 `m_IsActive` 跳过未激活的子系统，并解码 Texture2D（RGB24、RGBA32、ETC1、ETC2 + EAC）与 Mesh（顶点流、UV、顶点色、16/32 位索引）。每个 bundle 只下载、解析、转换一次，由所有用到它的条目共享。
- 通关烟花：原作 APK 内置（不在 AssetBundle 中），组件树快照 `game-clear-profile.json.gz` 与两张贴图随项目放在 `assets/apk/gameclear/`（`go:embed` 编入程序），每个条目都包含。

本生成器不使用 Bestdori。

## 转换

模块：Initial（含 3D 起始尺寸与旋转）、Emission（rate 与 burst）、Shape（球、半球、锥、box、圆）、Color、Size（含分轴）、Rotation（含分轴）、RotationBySpeed（按有效速度，含分轴）、Velocity over Lifetime（线性、轨道、径向、速度倍率，local / world）、Force over Lifetime、Limit Velocity（阻尼与阻力）、Texture Sheet Animation、Custom Data（UV 滚动 shader 使用）。模拟顺序与原生一致：重力 → 旋转 → Velocity → Force → 限速 → 速度倍率 → RotationBySpeed → 积分。

渲染：

| 原作 | 转换 |
|---|---|
| Billboard（View / Local 对齐） | 矩形；Local 对齐且只绕 X 倾斜的系统进 `#plane<角度>` 效果 |
| Stretched billboard | 沿速度拉伸的矩形（渲染对齐对拉伸粒子不起作用，总是面向相机） |
| Mesh：内置 Quad | 与 billboard 相同的矩形，但旋转为逆时针（mesh 旋转方向与 billboard 相反），并计入 renderer pivot；起始绕 X 的固定倾角与 transform 倾角合并为 `#plane<角度>` |
| Mesh：其他网格（witch 的 crossCylinder、screwTower、screwTowerLow） | 软件光栅化：网格按原作的尺寸、Euler ZXY 旋转、对齐基、pivot、贴图图块、材质缩放/偏移、Custom Data UV 滚动与顶点色渲染成精灵（加色材质累加，其余按 alpha 叠加），粒子在网格投影包围盒上绘制该精灵 |

Shader：`Mobile/Particles/Additive`（内置 10720，加色）、`Mobile/Particles/Alpha Blended`（内置 10721：同一贴图上名为 `…add` 的材质用 10720、`…alpha` 的用 10721）、`Legacy Shaders/Particles/Alpha Blended Premultiply`、`Particles/Standard Unlit`（混合取材质的 `_SrcBlend/_DstBlend`）、`star/Customdata_CurveUVScroll`（混合取 `_BlendSrc/_BlendDst`，UV 加 Custom Data 向量 0 的 (y, x)）。加色贴图换算为 alpha 混合。贴图按 RGBA 内容去重（各 bundle 中相同的 `Tex_parSet_1` 等共用精灵）。

### 近似

Sonolus 粒子是只做线性插值、只有线性随机输入的旋转矩形，以下部分只能近似：

- Unity 的非线性随机量（方位角以外的分布、寿命、速度、颜色、贴图帧、极角、部分弧等）按分层抽样拆成子组（每次爆发最多 4 层）；速度、尺寸、颜色曲线按每粒子最多 8 段线性逼近（位置容差 0.05 粒子单位）。
- 循环效果（`effect_TapKeep`）按 1 s 稳态周期输出（含旋转环的平面部分延长到整圈）。
- 绕 X / Y 旋转随时间变化、或不能并入平面的 billboard / Quad：输出保持投影后 X 边与投影面积的矩形，丢失剪切。
- 光栅化网格：朝向量化到 5°、UV 滚动量化到 1/64、尺寸比量化到 0.02，精灵最长边 192 px（3×3 超采样）；每段线性插值内朝向不变（每粒子最多 8 段）。网格不做深度排序，三角形按索引顺序叠加（原作 ZWrite 关闭）。
- 恰好侧对相机（倾角 ±90°）的平面粒子在原作正交相机下不可见，跳过并在契约 `notes` 中记录。
- 寿命超过 60 s 的常驻粒子只采样效果窗口内的部分；发射率异常高（每周期超过 10000 个）且受最大粒子数限制的系统按空出名额的间隔发射。
- billboard 的 renderer pivot 不计入（mesh 的计入）。

## 契约

`<out>/contracts/<条目>.json` 列出该条目的每个效果：`name`、`prefab`、`duration`、`loop`、`planeDeg` 与 `quadScale`（平面效果的纵向压缩 cos）、`groups`、`particles`、`systems`、`notes`（跳过的系统与原因）、HABAHIRO 宽度变体的 `width`、烟花的 `clear`。

不同 tap effect 的平面倾角不同（44°–64°、−65° 等），所以各条目的效果名集合不同：全部条目的效果名并集为 186 个（普通条目 96 个，HABAHIRO 条目 129 个），有的条目某个 prefab 只有平面部分、没有面向屏幕的部分。引擎声明并集，初始化时用 `Exists()` 记下当前条目有哪些部分（每个 prefab 最多两个平面部分）。

### 统一时长

Sonolus 粒子效果中的粒子时间是生成时长的比例，引擎读不到条目元数据，所以同一族效果在所有条目中使用同一个时长（`canonical.go`，引擎的 `dur*` 常量与之一致）：

| 族（含宽度变体与平面部分） | 时长 |
|---|---|
| `effect_tap`、`effect_tap_good`、`effect_tap_skill_good` | 0.8 s |
| `effect_tap_great`、`effect_tap_skill_great` | 0.85 s |
| `effect_tap_perfect`、`effect_tap_skill_perfect`、`effect_tap_swipe` | 1 s |
| `effect_TapKeep`（循环） | 1 s，平面部分 18 s |
| 方向键 `effect_tap_directional_flick_l/r`、`_2`、`_3`、`_finger` | 0.7 s、1.1 s、1.1 s、0.35 s |

一次性效果取该族在全部 tap effect 中最长的时长，较短的效果时间不变、提前结束；循环效果取各周期的公倍数（TapKeep 平面部分的周期为 1、6、9 s），较短的周期重复。某个效果超过该族时长或周期不能整除时生成失败，需要同时修改 `canonical.go` 与引擎常量。烟花在所有条目中相同，保留原时长。

## 烟花

`game-clear-profile.json` 是原始 Unity 组件树加 Animator clip。clip 在各自时刻激活烟花根对象（`effect_par_*`），根下的 ParticleSystem 随之播放。每个根（若根下系统的发射位置不同则按位置拆分，名字加 `#<首个系统名>`）输出一个效果，系统间的 startDelay 已包含在效果内：

- `GameClearParticle/effect_par_firework_star_<颜色>1[ (1)]`：基础通关烟花（10 个根 × 3 个系统 a/b/c；`green1` 拆为 `#effect_gameclear_firework_star_a` 与 `#effect_gameclear_firework_star_b`）。clip `MusicGameClear` 在通关后 0.2 s 开始。
- `FullComboAnimation/effect_par_firework_star`、`AllPerfectAnimation/effect_par_firework_star`：FC / AP 追加烟花，clip 在通关时开始，0.2333 s 激活。

契约中这些效果带 `clear` 字段：`anchorUI`（发射点，UI 单位，屏幕中心为原点、y 向上，已含子对象偏移 `0.185 × 子对象位置`）、`start`（通关后秒数）、`clipTime`（clip 内激活时间）、`unit`（0.185）。引擎在 `start` 时以 `uiPoint(anchorUI)` 为中心、半边长 `unit`（Sonolus 单位，**不**乘 UI 缩放：原作 scaling mode 为 Local，只用系统自身缩放 0.185）生成一次。base 用于所有通关，fullCombo / allPerfect 分别在状态 2 / 3 时追加。

## HABAHIRO

原作对 multi-range（`#HABAHIRO`）谱面的 tap effect 一律取 `ingameskin/tapeffect/habahiro`（`ButtonManager.getEffectSkinBundleName` 先于普通与特殊皮肤判断），方向键特效仍按设置。HABAHIRO 条目另追加宽度变体 `<prefab>_2` … `_7`（`effect_tap_good/great/perfect`、`effect_tap_skill_*`、`effect_tap_swipe`、`effect_TapKeep`；`effect_tap` 无变体）。宽度只改变横向光晕系统的 `startSize.x = 2.5·w`（及 `effect_TapKeep_*/par_square` 的尺寸曲线），契约中带 `width` 字段。基础名效果与 skin00 转换结果逐字节相同。

引擎对宽度 w 的音符使用 `<prefab>_<w>`（w = 1 用基础名），锚点为该音符的特效 lane（奇数宽取中心 lane，偶数宽取两条中心 lane 之间的半按钮），quad 与普通效果相同；Slide 的 TapKeep 始终用基础 `effect_TapKeep`。

## 作为 Go 库

模块根包 `particle` 提供与命令行相同的生成功能，不写文件、不退出进程、不使用 sonolus-pack-go，便于在服务器启动时一次性生成：

```go
import particle "github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator"

names, _ := particle.Items() // 将生成的条目名（顺序与 Generate 相同）
err := particle.Generate(ctx, particle.Options{
	Source: particle.Source{CacheDir: "cache/notgarupa"},
}, func(r *particle.Result) error {
	// 按 Items 的顺序逐个回调（串行）；返回错误即中止
	return nil
})
```

- `Options`：`Source`（`CacheDir`、`AssetVersion`（默认 `particle.PinnedAssetVersion`）、`Offline`、`HTTPClient`、`Log`）与 `Workers`（0 为 CPU 数）。`ctx` 可取消下载与转换。多个生成器可以共用同一个 `CacheDir`（缓存写入是原子的）。
- 每个 `*particle.Result`：`Item`（sonolus-core-go 的 `database.DatabaseParticleItem`，资源字段留空）、`Habahiro`、`Data`（`resource.ParticleData`）、`Texture`、`Thumbnail`（PNG）与 `Contract`。
- `Result.Files()` 按 item.json 的字段名返回资源字节："thumbnail"、"data"（已 gzip 压缩）、"texture"。
- `particle.CreateSourceTree(dir)` 新建 sonolus-pack 源目录，`(*SourceTree).Add(r)` 写入一个条目（命令行就是 `Generate` + `SourceTree` + sonolus-pack-go）。

在 sonolus-server-go 中注册（作为依赖同样需要 `GOPRIVATE`）：

```go
err := particle.Generate(ctx, opts, func(r *particle.Result) error {
	files, err := r.Files()
	if err != nil {
		return err
	}
	it := r.Item // database.DatabaseParticleItem，资源字段待填
	it.Thumbnail, it.Data, it.Texture = s.AddBytes(files["thumbnail"], ""), s.AddBytes(files["data"], ""), s.AddBytes(files["texture"], "")
	s.Particle.Items = append(s.Particle.Items, sonolusserver.ParticleItemModel{DatabaseParticleItem: it})
	return nil
})
```

公共代码（CDN 下载与缓存、Unity bundle / 贴图 / 网格解析、图像处理、打包输出等）放在本项目 `internal/` 下，项目可单独构建与运行。资源名称约定见 `sonolus-notgarupa-engine/docs/RESOURCE_CONTRACT.md`。
