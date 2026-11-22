#!/usr/bin/env bash
# 在 T16A 工具镜像内运行：验证 age 加密往返、截断密文与错误密钥都被拒绝。
# 不接触数据库、不写仓库；临时目录用完即删。
set -euo pipefail
work="$(mktemp -d "${TMPDIR:-/tmp}/t16a-age.XXXXXX")"
trap 'rm -rf -- "$work"' EXIT
umask 077

age --version
age-keygen --output "$work/identity.txt" 2>"$work/keygen.log"
recipient="$(age-keygen -y "$work/identity.txt")"
[[ "$recipient" == age1* ]] || { echo 'FAIL: recipient 生成失败'; exit 1; }

printf '合成样本：t16a age roundtrip 中文与 emoji 校验\n' > "$work/plain.txt"
plain_sha="$(sha256sum -- "$work/plain.txt" | awk '{print $1}')"

age --encrypt --recipient "$recipient" --output "$work/cipher.age" "$work/plain.txt"
[[ -s "$work/cipher.age" ]] || { echo 'FAIL: 密文为空'; exit 1; }
[[ "$(head -c 21 "$work/cipher.age")" == "age-encryption.org/v1" ]] || { echo 'FAIL: 密文头部不是 age v1'; exit 1; }

age --decrypt --identity "$work/identity.txt" --output "$work/roundtrip.txt" "$work/cipher.age"
roundtrip_sha="$(sha256sum -- "$work/roundtrip.txt" | awk '{print $1}')"
[[ "$plain_sha" == "$roundtrip_sha" ]] || { echo 'FAIL: 往返内容摘要不一致'; exit 1; }

head -c 40 "$work/cipher.age" > "$work/truncated.age"
if age --decrypt --identity "$work/identity.txt" "$work/truncated.age" >/dev/null 2>&1; then
    echo 'FAIL: 截断密文未被拒绝'; exit 1
fi

age-keygen --output "$work/other-identity.txt" 2>/dev/null
if age --decrypt --identity "$work/other-identity.txt" "$work/cipher.age" >/dev/null 2>&1; then
    echo 'FAIL: 错误私钥未被拒绝'; exit 1
fi

echo 'PASS age-roundtrip: encrypt/decrypt, truncated-ciphertext-rejected, wrong-key-rejected'
