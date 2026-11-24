# 连上服务器、登记服务器（情况乙、丙；装好后三种情况都要登记）

**什么时候读这一册**：用户要把 AcornFox 装到一台 Linux 服务器、而你不在那台服务器上（情况乙、丙）：先读这一册的“第 2 步：连上服务器”，再回到 `install-linux.md` 做第 3 步；服务器上装好以后，三种情况（甲、乙、丙）都要做“第 7 步：登记服务器”。用户说“ssh 连不上”“主机指纹”“免密登录”“跳板机”，或命令报 `diagnosis.stage` 是 `connect`（`host_key_unknown`、`permission_denied` 等）时也读这一册。这一册是 `install-linux.md` 整个流程里的第 2、7 步，步骤编号沿用那边的。

## 最低路径

### 第 2 步（情况乙、丙）：连上服务器

1. 向用户要 SSH 地址：`用户@地址`，以及端口（不是 22 时）、私钥路径（不是默认私钥时）。不要猜。
2. 测试能否免密码登录：

```bash
ssh -o BatchMode=yes -o ConnectTimeout=10 用户@地址 'echo ok'
```

端口不是 22 时加 `-p 端口`；用指定私钥时加 `-i 私钥路径`。后面每条 ssh 命令都照样加。

从上往下，第一条符合的就是结果：

| 看到 | 下一步 |
| --- | --- |
| `ok` | 继续 `install-linux.md` 第 3 步 |
| `REMOTE HOST IDENTIFICATION HAS CHANGED` | 服务器指纹和这台电脑记下的不一样（重装过系统，或者有人冒充）。**停下**问用户：“这台服务器最近重装过系统或换过机器吗？”用户确认重装过，才执行 `ssh-keygen -R 地址`（端口不是 22 时写 `ssh-keygen -R '[地址]:端口'`），再按下面“确认服务器指纹”做；用户说没有，就不要继续 |
| `Host key verification failed` | 这台电脑还没确认过服务器指纹。按下面“确认服务器指纹”做，做完重新测试。**不要用关闭指纹校验的参数绕过**（`StrictHostKeyChecking=no`、`accept-new`、`UserKnownHostsFile=/dev/null` 都不行） |
| `Permission denied` | 服务器只能用密码登录，要先把用户电脑的 SSH 公钥放到服务器上，按下面“配置免密登录”做 |
| `Connection timed out`、`No route to host`、`Connection refused` | 地址或端口不对，或者安全组没放行 22。请用户核对。服务器只能经跳板机进入时，按下面“经跳板机连接”做 |

**确认服务器指纹**（第一次连这台服务器时都要做一次）。指纹是服务器的“身份证号”，确认一次后存进这台电脑的 `~/.ssh/known_hosts`，以后连接会自动核对。两种做法，默认用做法一：

做法一（推荐）：请用户在**你执行命令的同一台电脑、同一个系统账号**上打开终端，运行（端口不是 22 时加 `-p 端口`）：

```bash
ssh 用户@地址
```

告诉用户：会看到 `ED25519 key fingerprint is SHA256:……` 和 `Are you sure you want to continue connecting (yes/no/[fingerprint])?`。核对指纹（方法见下）后输入 `yes` 回车。之后如果要输密码，可以直接按 Ctrl+C 退出；已经登录进去就输入 `exit` 退出。用户说做完了，你重新执行上面的免密登录测试。

做法二（用户希望你代办，或者你的终端不在用户电脑上时）：

```bash
mkdir -p ~/.ssh && chmod 700 ~/.ssh
ssh-keyscan -T 10 地址 2>/dev/null > ~/.ssh/acornfox-hostkeys.tmp
ssh-keygen -lf ~/.ssh/acornfox-hostkeys.tmp
```

端口不是 22 时写 `ssh-keyscan -T 10 -p 端口 地址`。最后一条会输出几行 `256 SHA256:…… 地址 (ED25519)`。没有任何输出就是地址、端口不对或安全组没放行，回到上表。把这几行原样给用户，请他核对；**用户明确说“一致”或“确认接受”之后**才执行：

```bash
cat ~/.ssh/acornfox-hostkeys.tmp >> ~/.ssh/known_hosts && rm ~/.ssh/acornfox-hostkeys.tmp
```

用户不确认就执行 `rm ~/.ssh/acornfox-hostkeys.tmp`，不要写入。

怎么核对指纹（告诉用户）：在云厂商控制台用“远程连接”（网页终端，不经过 SSH）登录服务器，运行 `for f in /etc/ssh/ssh_host_*_key.pub; do ssh-keygen -lf "$f"; done`，看 `(ED25519)` 那一行的 `SHA256:……` 和上面的是否一模一样。用户不方便核对、决定直接接受也可以，这由用户决定，你不要替他决定。

**经跳板机连接**（只有用户说服务器要先登录另一台“跳板机”才能进时做）：向用户要跳板机的 `用户@地址`（和端口）、目标服务器的内网地址和登录用户。两台都要能用密钥免密登录。告诉用户你要在他电脑的 `~/.ssh/config` 末尾加一段，用户同意后执行（按实际值替换；端口是 22、用默认私钥时可以删掉 `Port`、`IdentityFile` 两行）：

```bash
mkdir -p ~/.ssh && chmod 700 ~/.ssh
cat >> ~/.ssh/config <<'EOF'

Host acornfox-server
    HostName 目标服务器内网地址
    User 目标服务器用户
    Port 22
    IdentityFile ~/.ssh/id_ed25519
    ProxyJump 跳板机用户@跳板机地址:跳板机端口
EOF
chmod 600 ~/.ssh/config
```

之后本 Skill 里所有的 `用户@地址` 都换成 `acornfox-server`，并且不再加 `-p`、`-i`（已写在配置里）。指纹只能用做法一：请用户运行 `ssh acornfox-server`，会先后问两次（跳板机一次、目标服务器一次），都核对后输入 `yes`。跳板机要允许转发（sshd 的 `AllowTcpForwarding`），看到 `administratively prohibited` 或 `channel 0: open failed` 就是没允许，请用户找跳板机的管理员。

**配置免密登录**（只在上表看到 `Permission denied` 时做）：

1. 在用户电脑上执行 `ls ~/.ssh/id_*.pub`。
2. 没有任何输出（或报 `No such file`）：用户电脑上还没有 SSH 密钥。先告诉用户“我要在你电脑上生成一把 SSH 密钥，不需要输密码”，然后执行 `ssh-keygen -t ed25519 -N "" -f ~/.ssh/id_ed25519`。已经有 `.pub` 文件就跳过这一条，**不要覆盖已有的密钥**。
3. 请用户在自己的终端运行 `ssh-copy-id 用户@地址`（端口不是 22 时加 `-p 端口`），它会让用户输入一次服务器密码。**密码由用户自己输，你不能代输。**
4. 重新执行上面的免密登录测试。

| ssh-copy-id 时看到 | 下一步 |
| --- | --- |
| `No identities found` | 用户电脑上没有密钥，回到第 2 条生成 |
| `Permission denied` | 密码不对，或服务器关了密码登录。请用户到云厂商控制台重置密码，或用控制台的“远程连接”把公钥加进服务器的 `~/.ssh/authorized_keys` |
| Windows 上找不到 `ssh-copy-id` | 请用户在 Git Bash 里运行这条命令（Git Bash 自带） |

`install-linux.md` 后面“在服务器上执行”的命令，情况乙、丙一律在开头加 `ssh 用户@地址`，例如 `bash -s <<'EOF'` 写成 `ssh 用户@地址 bash -s <<'EOF'`。

情况丙在 Windows 上怎么运行 `acornfox` 命令，见入口“五种情况，命令在哪里运行”。装完这一步，回到 `install-linux.md` 的第 3 步。

### 第 7 步：登记服务器

**情况甲**（就在服务器上）。先在服务器上执行：

```bash
bash -s <<'EOF'
if [ "$(id -u)" != 0 ] && ! id -nG | tr ' ' '\n' | grep -qx acornfox-users; then if sudo -n usermod -aG acornfox-users "$(id -un)"; then echo "group=added"; else echo "group=need-password"; fi; else echo "group=ok"; fi
EOF
```

为什么要加组：命令行通过 `/run/acornfox/api.sock` 和后台服务说话，只有 root 和 `acornfox-users` 组的成员能连。安装脚本建好了这个组，但不会把你的账号加进去。

加组之后，在当前这个登录会话里执行 `id` 或 `id -nG` 看不到 `acornfox-users` 是**正常的**：新组只在之后新开的登录会话里生效，不是没加上。要核对有没有加上，执行 `getent group acornfox-users`，看末尾有没有你的账号。当前会话按下面 `group=added` 的写法（`sg` 或重新登录）继续。

- `group=ok`（或你是 root）：执行 `acornfox --json target add local --url unix:///run/acornfox/api.sock`，之后直接用 `acornfox` 命令。
- `group=need-password`：请用户在自己登录服务器的终端里运行 `sudo usermod -aG acornfox-users $(id -un)`（`$(id -un)` 是你登录服务器用的账号，用户登录的账号不同时，把它换成你的账号名），然后按 `group=added` 处理。
- `group=added`：组已经加上，但已经打开的会话（包括你现在这个）还不认新组。本次对话里每条 `acornfox` 命令都要包一层 `sg acornfox-users -c "…"`，例如：

```bash
sg acornfox-users -c "acornfox --json target add local --url unix:///run/acornfox/api.sock"
```

  用 `sg` 包着的命令看到下面的情况时，按表处理：

| 看到 | 下一步 |
| --- | --- |
| `"ok":true` | 正常，后面的 `acornfox` 命令都照这样包 |
| `sg: command not found`、`Password: Invalid password.`、其他以 `sg:` 开头的报错，或者仍然是 `"code":"permission_denied"` | 改用 `sudo -n -u "$(id -un)" acornfox …`（还是你自己的账号，sudo 会重新读取组，配置照样存在你自己的主目录），例如 `sudo -n -u "$(id -un)" acornfox --json target add local --url unix:///run/acornfox/api.sock`。后面的 `acornfox` 命令都照这样写 |
| 改用 `sudo -n -u` 后看到 `a password is required` | 你用不了 sudo。请用户退出、重新登录服务器，再重新打开（或重连）AI 工作台；新会话里直接用 `acornfox`，不用包 |

  不要用 `sudo acornfox …`（不带 `-u`）凑合：那样是以 root 身份运行，登记信息会存到 root 的目录里，之后你自己的账号用不了。

  也可以一开始就请用户退出、重新登录服务器后再打开 AI 工作台，之后就不用包了。

  如果你的每条命令其实都是新开一个 SSH 连接执行的（工作台通过 SSH 远程执行），新连接已经带上新组，不用包 `sg`：先直接试 `acornfox --json target add local --url unix:///run/acornfox/api.sock`，返回 `"ok":true` 就照常用；报 `permission_denied` 再按上面的办法包。

**情况乙**（通过 SSH，用户电脑是 Linux 或 Mac）。

1. 用户的电脑上先要有 CLI。执行 `acornfox --json version`（注意 `--json` 在 `version` 前面）：输出 `"ok":true` 就跳过安装；否则在用户电脑上安装（国内、海外按**用户电脑**所在网络选，拿不准就用 `install-linux.md` 第 3 步最后两行的 `curl` 在用户电脑上测一次；不需要 sudo）：

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash -s -- cli
```

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | bash -s -- cli
```

   输出提示安装目录不在 PATH 中时，本次对话里用输出给出的完整路径调用（例如 `~/.local/bin/acornfox`），并告诉用户把提示的那行加进 shell 配置。

2. SSH 用户不是 root 时，执行一次 `ssh 用户@地址 'sudo -n usermod -aG acornfox-users $(id -un)'`。`install-linux.md` 第 3 步是 `sudo=need-password` 时，请用户自己登录服务器运行 `sudo usermod -aG acornfox-users $(id -un)`。加组后经 SSH **新开**的连接立即生效：下一条 `target add` 和之后的每条命令都是新连接，不用重新登录，也不用包 `sg`。已经打开着的 SSH 会话（例如用户自己开着的终端）里执行 `id` 看不到新组是正常的，不代表没加上。
3. 登记（`my-server` 可以换成用户喜欢的名字；端口、私钥按需加；经跳板机的写 `--ssh acornfox-server`，不加 `--port`、`--identity`）：

```bash
acornfox target add my-server --ssh 用户@地址 [--port 端口] [--identity 私钥路径] --json
```

成功输出 `"ok":true`。失败按下面“连接出错”处理；最常见的是 `host_key_unknown`，说明第 2 步的“确认服务器指纹”没做，回去做完再登记。

**情况丙**（Windows，经 SSH 在服务器上运行）。服务器上的 `acornfox` 在 `install-linux.md` 第 5 步已经装好，Windows 电脑上不装。

1. SSH 用户不是 root 时，执行一次（`install-linux.md` 第 3 步是 `sudo=need-password` 时，请用户自己登录服务器运行 `sudo usermod -aG acornfox-users $(id -un)`）：

```bash
ssh 用户@地址 'sudo -n usermod -aG acornfox-users $(id -un)'
```

2. 在服务器上登记它自己（重复执行没关系）：

```bash
ssh 用户@地址 'acornfox --json target add local --url unix:///run/acornfox/api.sock'
```

成功输出 `"ok":true`，之后的 `acornfox` 命令都按入口“五种情况，命令在哪里运行”里情况丙的写法执行。看到 `"code":"permission_denied"`：第 1 条加组没做成，回去做；加组后新开的 ssh 连接立即生效，不用重新登录，也不用包 `sg`。

登记好以后，装在服务器上的流程接着做“第 7 步之后：打开控制台”（`access.md`）。

## 连接出错

命令输出里 `diagnosis.stage` 是 `connect` 时，`diagnosis.code`（`ssh_missing`、`host_unreachable`、`auth_failed`、`host_key_unknown`、`host_key_changed`、`acornfox_missing`、`permission_denied`、`server_down`、`version_mismatch` 等）照入口“遇到故障编号怎么办”运行 `acornfox faults 诊断码 --json`。和这一册的步骤有关的几条：

- `host_key_unknown`：这台电脑还没确认过服务器指纹，按第 2 步“确认服务器指纹”做，做完重新执行刚才的命令。
- `host_key_changed`：服务器指纹变了（重装过系统，或者有人冒充）。**停下**问用户是不是重装过；用户确认重装过，才按第 2 步表格里 `REMOTE HOST IDENTIFICATION HAS CHANGED` 那一行处理，否则不要动 known_hosts。
- `auth_failed`：免密登录不通，回到第 2 步。
- `permission_denied`：账号不在 `acornfox-users` 组，按第 7 步加组；情况乙、丙加完组后直接重试，情况甲按第 7 步用 `sg`、`sudo -n -u` 或重新登录。
- `acornfox_missing`：服务器上没装 AcornFox，回到 `install-linux.md` 第 3 步。
- `server_down`、`local_server_down`：服务没在运行，做 `install-linux.md` 第 6 步自检。
- `version_mismatch`：电脑上的命令行和服务器版本对不上，重新运行第 7 步的 install-cli 命令升级命令行；服务器旧就按 `maintain.md` 的升级处理（情况丙没有电脑上的命令行，不会出现）。
- `forwarding_disabled`、`connect_failed`：转述 `message` 和 `hint`。

## 可以发挥的地方

- 用大白话向用户解释 SSH 指纹是什么、为什么要核对、为什么用免密登录；帮用户判断服务器该怎么连（直连还是经跳板机）。
- 用户电脑上已经有 SSH 配置、别名时，用现成的别名，不要重复写。

## 必须先问用户的

- 服务器指纹：只有用户明确说“一致”或“确认接受”才写进 `known_hosts`；`ssh-keygen -R` 之前先问是不是重装过系统。**不要用关闭指纹校验的参数绕过。**
- 密码（SSH 密码、sudo 密码、`ssh-copy-id` 要输的密码）：只能用户自己输，你不能代输。
- 在用户电脑上生成 SSH 密钥（先说明，不要覆盖已有的密钥）、往 `~/.ssh/config` 末尾加跳板机配置（用户同意后才做）。
- 把用户加进 `acornfox-users` 组需要 sudo：`sudo=need-password` 时交给用户自己运行。
- 在用户电脑上安装命令行（会换掉电脑上的程序）。
