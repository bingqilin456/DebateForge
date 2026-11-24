package ai

func ValidDifficulty(s string) bool {
	return s == "beginner" || s == "varsity" || s == "national"
}

func NormalizeDifficulty(s string) string {
	if ValidDifficulty(s) {
		return s
	}
	return "beginner"
}

func difficultyRequirement(difficulty string) string {
	switch difficulty {
	case "beginner":
		return "入门档回复目标为 600–800 字，重点解释一至两个反驳。\n"
	case "varsity":
		return "校队档回复目标为 800–1100 字，逐项回应标准、证据和推理。\n"
	case "national":
		return "国赛档回复目标为 1000–1400 字，回应核心逻辑并比较两方论证负担。\n"
	default:
		return ""
	}
}
