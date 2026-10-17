package main

import "testing"

// TestInline checks the HTML of a line of text: the signs of markup escaped,
// code and links, and what is shown as written.
func TestInline(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"plain", "Корзина сохраняется.", "Корзина сохраняется."},
		{"signs of markup", `<b>Итого</b> & "цена"`, "&lt;b&gt;Итого&lt;/b&gt; &amp; &#34;цена&#34;"},
		{"code", "Команда `shop promo add` есть.", "Команда <code>shop promo add</code> есть."},
		{"signs in code", "`<артикул> & <цена>`", "<code>&lt;артикул&gt; &amp; &lt;цена&gt;</code>"},
		{"a ` with no pair", "a ` b", "a ` b"},
		{"link", "[в справке](https://shop.example/help).", `<a href="https://shop.example/help">в справке</a>.`},
		{"link http", "[тут](http://shop.example)", `<a href="http://shop.example">тут</a>`},
		{"code in a link", "[`shop`](https://shop.example)", `<a href="https://shop.example"><code>shop</code></a>`},
		{"address escaped", `[a](https://shop.example/?a=1&b="2")`, `<a href="https://shop.example/?a=1&amp;b=&#34;2&#34;">a</a>`},
		{"link to another scheme", "[a](javascript:alert(1))", "[a](javascript:alert(1))"},
		{"relative link", "[a](help.html)", "[a](help.html)"},
		{"brackets that start no link", "[a] и [b](https://shop.example)", `[a] и <a href="https://shop.example">b</a>`},
		{"a link with no end", "[a](https://shop.example", "[a](https://shop.example"},
		{"an address with a space", "[a](https://shop.example x)", "[a](https://shop.example x)"},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(inline(tc.text)); got != tc.want {
				t.Errorf("inline(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}
