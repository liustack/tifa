// Ported from products/review.go: the texts the generated reviews are made of.
import type { Text } from '../store.ts'

// reviewTags are what reviews mention, the most mentioned first.
export const reviewTags: Text[] = [	["做工扎实", "Well made"], ["颜色好看", "Lovely colour"], ["尺寸合适", "Just the right size"],
	["包装用心", "Thoughtful packaging"], ["适合送礼", "Good as a gift"], ["物流较快", "Quick delivery"], ["性价比高", "Good value"],]

export const reviewers: Text[] = [	["林夏", "Lin Xia"], ["陈默", "Mo Chen"], ["周宁", "Ning Zhou"], ["许知", "Zhi Xu"],
	["苏禾", "He Su"], ["何安", "An He"], ["沈星", "Xing Shen"], ["江屿", "Yu Jiang"],
	["Emma W.", "Emma W."], ["Noah K.", "Noah K."], ["Olivia R.", "Olivia R."], ["Liam P.", "Liam P."],]

export const reviewTexts: { title: Text; body: Text }[] = ([	[["比想象中更好", "Better than I expected"], [
		"收到后第一感觉是做工很扎实，细节处理得很干净。用了两周，每天都在用，没有任何问题。包装也很用心，送人也合适。",
		"The first thing I noticed was how solid it feels, and how clean the details are. Two weeks of daily use and no trouble at all. The packaging is lovely too, good enough for a gift.",
	]],
	[["颜色很温柔", "Such a gentle colour"], [
		"实物颜色比图片更柔和一点，放在家里很协调。唯一的小遗憾是物流慢了一天，不过客服提前通知了。",
		"The colour is a touch softer than in the photos and fits right in at home. My only gripe is that delivery was a day late, but the shop told me ahead of time.",
	]],
	[["已经回购第二次", "My second one already"], [
		"第一次买给自己，这次买给妈妈。她很喜欢，说用起来顺手。希望以后能出更多颜色。",
		"I bought the first for myself and this one for my mother. She loves it and says it is easy to use. I hope more colours come out.",
	]],
	[["性价比不错", "Good value"], [
		"价格不算便宜，但用料和手感对得起这个价。和之前在别家买的相比，明显更耐用。",
		"Not cheap, but the materials and the feel are worth it. It has clearly held up better than the one I bought elsewhere.",
	]],
	[["有一点小瑕疵", "A small flaw"], [
		"整体满意，但边缘有一处很小的痕迹。联系客服后很快就给了补偿，处理态度很好。",
		"Happy overall, but there was a tiny mark on one edge. The shop made it right quickly and was very kind about it.",
	]],
	[["每天都离不开", "I use it every day"], [
		"已经成了我早晨的固定流程。尺寸刚好，不占地方。清洗也方便，几秒钟就好。",
		"It is part of my morning now. Just the right size and it takes no room. Easy to clean, too: a few seconds and done.",
	]],
	[["送礼很体面", "A gift that looks the part"], [
		"买来送同事，礼盒很精致，拿出手很体面。同事说质感很好，问我在哪里买的。",
		"I gave it to a colleague. The box is beautiful and it looked great. She said it feels lovely and asked where I got it.",
	]],] as [Text, Text][]).map(([title, body]) => ({ title, body }))
