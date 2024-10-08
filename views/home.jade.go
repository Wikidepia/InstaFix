// Code generated by "jade.go"; DO NOT EDIT.

package views

import (
	"io"
)

const (
	home__0 = `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"/><meta name="viewport" content="width=device-width, initial-scale=1"/><meta property="og:title" content="InstaFix"/><meta property="og:site_name" content="InstaFix"/><meta property="og:description" content="Fix Instagram embeds in Discord (and Telegram!)"/><title>InstaFix</title><link rel="icon" href="data:image/svg+xml,&lt;svg xmlns=&#39;http://www.w3.org/2000/svg&#39; viewBox=&#39;0 0 100 100%&#39;&gt;&lt;text y=&#39;.9em&#39; font-size=&#39;90&#39;&gt;🛠&lt;/text&gt;&lt;/svg&gt;"/><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/@picocss/pico@1.5.13/css/pico.min.css"/></head><body><main class="container" style="max-width: 35rem"><hgroup><h1>InstaFix</h1><h2>Fix Instagram embeds in Discord (and Telegram!)</h2></hgroup><p>InstaFix serves fixed Instagram image and video embeds. Heavily inspired by fxtwitter.com.</p><section><header><h3 style="margin-bottom: 4px">How to Use</h3><ul><li>Add dd before instagram.com to fix embeds, or</li><li>`
	home__1 = `</li></ul></header><video src="https://user-images.githubusercontent.com/72781956/168544556-31009b0e-62e8-4d4c-909b-434ad146e118.mp4" controls="controls" muted="muted" style="width: 100%; max-height: 100%">Your browser does not support the video tag.</video><hr/><small><a href="https://github.com/Wikidepia/InstaFix" target="_blank">Source code available in GitHub!</a></small><br/><small>• Instagram is a trademark of Instagram, Inc. This app is not affiliated with Instagram, Inc.</small></section></main></body></html>`
)

func Home(wr io.Writer) {
	buffer := &WriterAsBuffer{wr}

	buffer.WriteString(home__0)

	buffer.WriteString(`To get direct media embed, add ` + "`" + `d.dd` + "`" + ` before ` + "`" + `instagram.com` + "`" + `.`)
	buffer.WriteString(home__1)

}
