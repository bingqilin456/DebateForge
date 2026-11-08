package auth

// UserDTO 是契约中的 User（`id` 十进制字符串 + 归一化 `username`），
// 刻意不含 password_hash 等内部字段，避免序列化泄露。
type UserDTO struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// AuthDTO 是登录与当前用户共用的 AuthData：用户信息 + 当前 Session 的 CSRF token。
// Session 原值只通过 Set-Cookie 传输，绝不进入 JSON。
type AuthDTO struct {
	User      UserDTO `json:"user"`
	CSRFToken string  `json:"csrf_token"`
}

// newUserDTO 把领域用户映射为对外 DTO；ID 为 0 说明数据异常，返回错误而不是输出 "0"。
func newUserDTO(user User) (UserDTO, error) {
	id, err := FormatEntityID(user.ID)
	if err != nil {
		return UserDTO{}, err
	}
	return UserDTO{ID: id, Username: user.Username}, nil
}
