# 11 — Mảng sắp xếp và binary search

Package: [`internal/store`](../internal/store/search.go)

## Mục tiêu

Ba tính năng còn thiếu — **quét bảng**, **truy vấn khoảng** (`a > 123`), **sắp xếp**
(`order by`) — nghe như ba thứ khác nhau, nhưng thật ra là một:

| Việc | Thực chất |
|---|---|
| Quét cả bảng | khoảng từ −∞ tới +∞ |
| Truy vấn khoảng | tìm hai đầu mút, rồi duyệt |
| `order by` | duyệt xuôi hay duyệt ngược |

Cả ba đều là **duyệt theo thứ tự**. Mà `map` thì trả lời được đúng một câu hỏi: "dưới key
này có gì". Nó không biết "cái tiếp theo là gì", và không có cách nào bắt nó biết.

Nên bước này đổi ruột của `store`: `map[string][]byte` thành hai mảng sắp xếp.

```go
type Store struct {
	mu   sync.RWMutex
	keys [][]byte      // đã sắp xếp, không trùng
	vals [][]byte      // vals[i] thuộc về keys[i]
}
```

## Binary search: viết tay một lần

Thư viện chuẩn có `slices.BinarySearchFunc`, nhưng đây là loại hàm nên tự viết ít nhất
một lần trong đời. Nhìn thì đơn giản, mà số người viết sai nó thì rất nhiều.

```go
func search(keys [][]byte, key []byte) (pos int, found bool) {
	lo, hi := 0, len(keys)
	for lo < hi {
		mid := lo + (hi-lo)/2
		switch cmp := bytes.Compare(keys[mid], key); {
		case cmp < 0:
			lo = mid + 1
		case cmp > 0:
			hi = mid
		default:
			return mid, true
		}
	}
	return lo, false
}
```

### Bất biến của vòng lặp

Chỉ có một câu, và mọi thứ khác suy ra từ nó:

> Nếu `key` có nằm đâu đó, thì nó nằm trong `keys[lo:hi]`.

Mọi phần tử trước `lo` đã được chứng minh là **nhỏ hơn**; mọi phần tử từ `hi` trở đi đã
được chứng minh là **lớn hơn**. Khi `lo` gặp `hi`, khoảng rỗng — key không có ở đó — và
`lo` chính là **đường nối giữa phần nhỏ hơn và phần lớn hơn**, tức là chỗ phải chèn vào.

Đó là lý do một lần tìm trả lời được **cả hai** câu hỏi mà một lệnh ghi cần hỏi: *có sẵn
chưa*, và *nếu chưa thì đặt vào đâu*. Không tốn thêm phép so sánh nào.

### Vì sao khoảng nửa mở

`hi` là **một quá** phần tử cuối cùng, không phải phần tử cuối cùng. Nhờ vậy:

- `hi = len(keys)` là giá trị khởi tạo hợp lệ, không cần `len-1`.
- Mảng rỗng không cần xử lý riêng: vòng lặp không chạy, trả về `0, false`.
- Key lớn hơn mọi thứ cũng không cần xử lý riêng: trả về `len(keys), false` — đúng chỗ
  phải append.

Viết theo kiểu `lo <= hi` với `hi = len-1` thì cả ba trường hợp trên đều thành một case
riêng, và đó là nơi lỗi off-by-one sinh sống.

### `lo + (hi-lo)/2` chứ không phải `(lo+hi)/2`

Với `int` 64 bit thì tổng không thể tràn trên bất kỳ mảng nào vừa bộ nhớ, nên ở đây nó
thuần túy là thói quen. Nhưng đó là thói quen đáng có: phiên bản cộng vào rồi chia đôi
từng nằm **sai suốt nhiều năm trong thư viện chuẩn của Java**, và không ai phát hiện ra
vì phải hơn một tỷ phần tử mới lộ.

### Test

Ba tầng, vì đây là hàm đáng test kỹ:

| Test | Cách |
|---|---|
| `TestSearch` | các ca viết tay: rỗng, đầu, cuối, ở giữa, ngoài hai đầu, độ dài chẵn |
| `TestSearchExhaustively` | **mọi tập con** của 8 phần tử (256 tập), tìm mọi phần tử, đối chiếu với vòng lặp tuyến tính |
| `TestSearchAtEveryLength` | mọi độ dài từ 0 tới 300, tìm cả key có thật lẫn mọi khe trống |

Ca vét cạn là ca đáng giá nhất: 256 hình dạng mảng nhân với 8 mục tiêu, và mỗi lần đều
kiểm thêm rằng `pos` là vị trí **giữ được thứ tự** — thứ mà lệnh chèn dựa vào.

## Hai mảng song song, không phải một mảng cặp

```go
keys [][]byte
vals [][]byte
```

thay vì

```go
entries []struct{ key, val []byte }
```

Vì binary search chỉ đụng tới **key**. Để key nằm liền nhau trong bộ nhớ thì mỗi dòng
cache kéo lên được nhiều key hơn; để xen kẽ key với value thì một nửa số byte kéo lên là
thứ phép tìm không dùng tới.

Cái giá là **mọi lệnh ghi phải dời cả hai mảng, hoặc không dời cái nào**. Lệch một nhịp là
value của key này gắn sang key khác — sai âm thầm, không có gì báo.

## Chi phí đổi lại

| | `map` | Mảng sắp xếp |
|---|---|---|
| `Get` | O(1) | O(log n) |
| `Set` key đã có | O(1) | O(log n) — chỉ đổi value, không dời gì |
| `Set` key mới | O(1) | **O(n)** — dời mọi phần tử phía sau |
| `Delete` | O(1) | **O(n)** |
| Duyệt theo thứ tự | không làm được | O(1) mỗi bước |

Hàng cuối là thứ vừa mua được, và bốn hàng trên là giá phải trả. Đổi được không, tùy vào
việc dòng cuối có đáng hay không — với một database quan hệ thì đáng, vì thiếu nó thì
`order by` và range query không tồn tại.

### Khởi động chậm hẳn đi

Replay chèn từng record một, mỗi lần O(n), nên tổng cộng là **O(n²)**. Số đo thật trên máy
này:

| Số record | Mở mất |
|---|---|
| 5.000 | 48 ms |
| 10.000 | 283 ms |
| 20.000 | 700 ms |

So với `map` trước đây: 20.000 record mở trong 189 ms. Chậm gấp 3,7 lần, và tỉ lệ bậc hai
nên 100.000 record sẽ mất khoảng 17 giây.

Cách chữa hiển nhiên là gom hết record rồi **sort một lần** ở cuối, thành O(n log n).
Không làm ở đây, vì nó phải viết lại ý nghĩa của `apply` ở một chỗ thứ hai — "record này
tác động lên bộ nhớ thế nào" hiện được định nghĩa đúng một lần, và [04](04-write-ahead-log.md#kv)
giải thích vì sao đó là điều đáng giữ. Ngoài ra LSM-tree sắp tới sẽ thay toàn bộ chỗ này.

## Key giờ cũng phải copy

`map[string][]byte` cho copy key miễn phí: `string` bất biến, nên `s.data[string(key)]`
là một bản riêng dù muốn hay không. Mảng `[][]byte` thì không — nếu giữ nguyên slice của
người gọi, người gọi tái dùng buffer là **các key đã lưu tự đổi nội dung**, và mảng thôi
không còn sắp xếp nữa.

`TestKeyIsCopied` giữ chỗ đó. Đây là loại lỗi mà `map` che giấu suốt và chỉ lộ ra khi đổi
cấu trúc.

Đổi lại, `kv.go` **bớt** một phép chuyển đổi: trước đây mỗi `Get` phải làm `string(key)` —
một lần cấp phát và copy — giờ thì đưa thẳng `[]byte` xuống.

## Sắp xếp rồi vẫn chưa range query được

Đây là chỗ phải nói rõ: mảng đã có thứ tự, nhưng **thứ tự đó chưa đúng thứ tự người dùng
mong đợi**. Key của một hàng do `Row.EncodeKey` ở [06](06-crud.md) sinh ra, mà cách mã hóa
đó dùng little-endian và bù hai:

```
int64  sắp bytewise: [256 1 2 10 1000 -1]     đúng ra phải là [-1 1 2 10 256 1000]
string sắp bytewise: [b z aa aaa]             đúng ra phải là [aa aaa b z]
```

Chuỗi sai vì độ dài đứng trước nội dung, nên `"z"` (dài 1) xếp trước `"aa"` (dài 2).

Trước khi có `where a > 123` hay `order by`, phải có một cách mã hóa key **so sánh được
bằng bytes**:

- `int64`: big-endian, rồi lật bit dấu (`XOR 0x80` lên byte đầu). Khi đó `-1` thành
  `7F FF…`, `0` thành `80 00…`, thứ tự byte khớp thứ tự số.
- `string`: bỏ length prefix, escape `0x00` thành `00 FF`, kết thúc cell bằng `00 00`.
  Vừa đúng thứ tự vừa vẫn tự phân định ranh giới.

Và đi kèm là `Row.DecodeKey`: khi quét bảng thì **không ai đưa khóa vào** như `Select`
hiện nay, nên cột khóa phải giải mã ngược ra từ chính key — chúng không nằm trong value.

Đó là món nợ đã ghi từ [05](05-data-types.md#giới-hạn-hiện-tại), và là bước kế tiếp.

## Giới hạn hiện tại

- **Chưa có API duyệt.** Cấu trúc đã sắp xếp nhưng `store` chưa có `Seek`/`Next`. Không có
  nó thì `Get`/`Set`/`Del` vẫn là tất cả những gì tầng trên nhìn thấy, và không có gì thay
  đổi từ góc nhìn của SQL.
- **Key chưa so sánh được bằng bytes.** Xem phần trên. Đây là thứ chặn đường thật sự.
- **Khởi động O(n²).** Số đo ở trên.
- **`SetEx` tìm hai lần.** `kv.SetEx` gọi `mem.Has` rồi `apply` gọi `mem.Set`, mỗi lần một
  binary search. Sách gộp làm một vì `keys` nằm thẳng trong `KV`; ở đây `store` là một
  package riêng nên phải trả giá đó. Là hệ số log, nhỏ hơn hẳn cái O(n) của phép chèn, nên
  chưa đáng phá ranh giới để tối ưu.
- **Vẫn phải vừa RAM.** Không đổi. LSM-tree là thứ gỡ chuyện này, và cũng là thứ sẽ thay
  cái mảng vừa dựng.
