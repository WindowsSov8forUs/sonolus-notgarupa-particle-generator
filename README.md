# sonolus-notgarupa-particle-generator

把原作的判定粒子 prefab（tap effect skin00 与方向键 skin00 normal，Unity ParticleSystem 参数）以及通关 / Full Combo / All Perfect 烟花转换为 Sonolus 粒子包。每个 prefab 输出一个面向屏幕的效果，以及每个 lane 平面倾角一个 `<prefab>#plane<角度>` 效果（四边形纵向按 cos 压缩）。运行时写出 `contract.json`（效果名、持续时间、平面角、烟花的生成位置与时间）。

## 命令行

从 GitHub Release 下载对应平台的压缩包（linux / windows / darwin，amd64 / arm64；由 `.github/workflows/build.yml` 矩阵编译，推送 `v*` 标签时发布），解压后直接运行；也可以用 Go 安装或在本仓库中运行（仓库为私有，需要 `GOPRIVATE=github.com/WindowsSov8forUs/*` 和可访问仓库的 git 凭据）：

```sh
go install github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator/cmd/sonolus-notgarupa-particle-generator@latest
go run ./cmd/sonolus-notgarupa-particle-generator [参数]   # 在本仓库中
```

```sh
sonolus-notgarupa-particle-generator [-variant normal|habahiro] [-strata 4] [-segments 8] [-tolerance 0.05] [-loop 1] [-standard] [-name notgarupa-skin00] \
    [-asset-version <CDN 资源版本>] [公共参数]
```

程序内置所需的 APK 资源，其余素材联网下载到 `-cache`（默认当前目录的 `.cache/`）。`-version` 输出版本号。

| `-variant` | 粒子项 | 输出 | 契约 |
|---|---|---|---|
| `normal`（默认） | `notgarupa-skin00` | `out/source`、`out/pack` | `contract.json` |
| `habahiro` | `notgarupa-habahiro` | `out/source-habahiro`、`out/pack-habahiro` | `contract-habahiro.json` |

`-name`、`-out`、`-pack`、`-contract` 显式给出时覆盖上表的默认值。

转换方式：Sonolus 粒子只有线性随机，Unity 的非线性随机量（方位角、尺寸、旋转、box 发射位置等）按分层抽样拆成多个子组；速度/尺寸/颜色曲线分段线性逼近；拉伸 billboard 与平面粒子单独处理；循环效果（`effect_TapKeep`）按稳态周期输出；未激活的 13 个子系统跳过。`-standard` 额外输出 Sonolus 标准效果名。贴图按 RGBA 内容去重（各 bundle 中相同的 `Tex_parSet_1` 等共用精灵）。

## 素材来源

- 判定粒子：从原作官方资源 CDN（`https://content.garupa.jp/Release/<版本>/Android/<bundle>`）下载 AssetBundle `ingameskin/tapeffect/skin00`、`ingameskin/tapeffect/directionalflickskin00normal`、`ingameskin/tapeffect/habahiro`，资源版本固定为 `internal/cdn` 的 `PinnedVersion`（`10.1.0.200_e019f5bb…`）。程序直接解析 UnityFS（LZ4）与带 type tree 的序列化文件，读取 GameObject / Transform / ParticleSystem / ParticleSystemRenderer / Material，按 `m_IsActive` 跳过未激活的子系统，并解码 Texture2D（RGBA32、ETC1、ETC2 + EAC）。下载缓存在 `.cache/cdn/<版本>/`。
- 通关烟花：原作 APK 内置（不在 AssetBundle 中），组件树快照 `game-clear-profile.json.gz` 与两张贴图随项目放在 `assets/apk/gameclear/`（`go:embed` 编入程序）。

本生成器不使用 Bestdori。

## 烟花

`game-clear-profile.json` 是原始 Unity 组件树加 Animator clip。clip 在各自时刻激活烟花根对象（`effect_par_*`），根下的 ParticleSystem 随之播放。每个根（若根下系统的发射位置不同则按位置拆分，名字加 `#<首个系统名>`）输出一个效果，系统间的 startDelay 已包含在效果内：

- `GameClearParticle/effect_par_firework_star_<颜色>1[ (1)]`：基础通关烟花（10 个根 × 3 个系统 a/b/c；`green1` 拆为 `#effect_gameclear_firework_star_a` 与 `#effect_gameclear_firework_star_b`）。clip `MusicGameClear` 在通关后 0.2 s 开始。
- `FullComboAnimation/effect_par_firework_star`、`AllPerfectAnimation/effect_par_firework_star`：FC / AP 追加烟花，clip 在通关时开始，0.2333 s 激活。

`contract.json` 中这些效果带 `clear` 字段：`anchorUI`（发射点，UI 单位，屏幕中心为原点、y 向上，已含子对象偏移 `0.185 × 子对象位置`）、`start`（通关后秒数）、`clipTime`（clip 内激活时间）、`unit`（0.185）。引擎在 `start` 时以 `uiPoint(anchorUI)` 为中心、半边长 `unit`（Sonolus 单位，**不**乘 UI 缩放：原作 scaling mode 为 Local，只用系统自身缩放 0.185）生成一次。base 用于所有通关，fullCombo / allPerfect 分别在状态 2 / 3 时追加。

## HABAHIRO

`-variant habahiro` 的 tap effect 取自 CDN bundle `ingameskin/tapeffect/habahiro`，其余（方向键、烟花、标准名）与普通粒子项相同，另追加宽度变体 `<prefab>_2` … `_7`（`effect_tap_good/great/perfect`、`effect_tap_skill_*`、`effect_tap_swipe`、`effect_TapKeep`；`effect_tap` 无变体）。宽度只改变横向光晕系统的 `startSize.x = 2.5·w`（及 `effect_TapKeep_*/par_square` 的尺寸曲线），契约中带 `width` 字段。基础名效果与 skin00 转换结果逐字节相同。贴图从该 bundle 解码，RGBA 与 skin00 相同，按内容去重后共用精灵。

引擎对宽度 w 的音符使用 `<prefab>_<w>`（w = 1 用基础名），锚点为该音符的特效 lane（奇数宽取中心 lane，偶数宽取两条中心 lane 之间的半按钮），quad 与普通效果相同；Slide 的 TapKeep 始终用基础 `effect_TapKeep`。

## 作为 Go 库

模块根包 `particle` 提供与命令行相同的生成功能，不写文件、不退出进程、不使用 sonolus-pack-go，便于嵌入服务器：

```go
import particle "github.com/WindowsSov8forUs/sonolus-notgarupa-particle-generator"

r, err := particle.Generate(ctx, particle.Options{
	Variant: particle.Habahiro, // 零值 Options 即默认的 notgarupa-skin00
	Source:  particle.Source{CacheDir: "cache/notgarupa"},
})
```

- `Options` 的零值即命令行的默认值：`Variant`（`particle.Normal` / `particle.Habahiro`）、`Name`、`Strata`、`Segments`、`Tolerance`、`LoopSeconds`、`NoStandardNames`。
- `Source` 控制下载：`CacheDir`、`AssetVersion`（默认 `particle.PinnedAssetVersion`）、`Offline`、`HTTPClient`、`Log`；`Generate` 的 `ctx` 可取消下载。多个生成器可以共用同一个 `CacheDir`（缓存写入是原子的）。
- 返回 `*particle.Result`：`Item`（sonolus-core-go 的 `database.DatabaseParticleItem`，资源字段留空）、Data（`resource.ParticleData`）、Texture、Thumbnail（PNG）与 Contract（即 `contract.json` 的内容）。
- `Result.Files()` 按 item.json 的字段名返回资源字节："thumbnail"、"data"（已 gzip 压缩）、"texture"。
- `particle.WriteSource(dir, results...)` 把结果写成 sonolus-pack 源目录（命令行就是 `Generate` + `WriteSource` + sonolus-pack-go）。

在 sonolus-server-go 中注册（作为依赖同样需要 `GOPRIVATE`）（资源字段填入仓库地址）：

```go
files, err := r.Files()
if err != nil { /* … */ }
it := r.Item // database.DatabaseParticleItem，资源字段待填
it.Thumbnail, it.Data, it.Texture = s.AddBytes(files["thumbnail"], ""), s.AddBytes(files["data"], ""), s.AddBytes(files["texture"], "")
s.Particle.Items = append(s.Particle.Items, sonolusserver.ParticleItemModel{DatabaseParticleItem: it})
```

## 公共参数（命令行）

| 参数 | 默认值 | 作用 |
|---|---|---|
| `-out` | `out/source` | 重新生成的 sonolus-pack 源目录 |
| `-pack` | `out/pack` | 用 sonolus-pack-go 打包到该目录（空字符串则跳过） |
| `-cache` | `.cache` | 下载缓存目录 |
| `-offline` | false | 只使用缓存中的下载 |
| `-v` | false | 输出下载日志 |
| `-version` | false | 输出版本号后退出 |

公共代码（CDN 下载与缓存、Unity bundle 解析、图像处理、打包输出等）放在本项目 `internal/` 下，项目可单独构建与运行。资源名称约定见 `sonolus-notgarupa-engine/docs/RESOURCE_CONTRACT.md`。
