# 01 — Tổng quan

## Mục tiêu

Viết một key-value database từ con số không bằng Go, để hiểu thứ mà bình thường ta chỉ
gọi API: dữ liệu nằm ở đâu, làm sao nó sống sót qua một lần tắt máy, làm sao nhiều
client cùng ghi mà không giẫm lên nhau.

Ưu tiên là hiểu rõ từng lớp, không phải chạy nhanh. Mỗi bước làm nhỏ, có test, có ghi chú
lý do — rồi mới sang bước sau.

## Cấu trúc

```
mydb/
├── main.go                    # demo tạm thời: mở db, ghi một key rồi thoát
├── Makefile                   # build / test / run
├── docs/                      # tài liệu, đọc theo thứ tự số
└── internal/
    ├── store/                 # [02] KV trong RAM: map + RWMutex
    ├── wal/                   # [03] định dạng record  [04] file log append-only
    └── kv/                    # [04] ghép log với store: ghi log trước, replay khi mở
```

`internal/` là quy ước của Go: package nằm trong đó chỉ import được từ trong chính module
này. Nó giữ cho phần bên trong được tự do thay đổi — chưa có ai ngoài repo phụ thuộc vào
nó để mà làm hỏng.

## Cách chạy

```sh
make run     # chạy demo trong main.go
make test    # chạy toàn bộ test
make check   # fmt + vet + test — chạy cái này trước khi commit
make help    # xem tất cả target
```

Xem thêm `make cover` (báo cáo coverage dạng HTML) và `make test-race`
(cần CGO và một trình biên dịch C; hiện chưa chạy được trên máy dev vì thiếu gcc).

## CI

`.github/workflows/ci.yml` chạy vet + test trên Ubuntu, macOS và Windows mỗi lần push.

Nó tồn tại vì hai đoạn code không thể kiểm chứng trên máy dev Windows:

- **`syncDir`** — bản Unix (fsync thư mục cha) luôn là hàm rỗng trên Windows, nên chỉ
  Ubuntu và macOS mới thật sự chạy nó. Xem [04](04-write-ahead-log.md#fsync-cả-thư-mục-cha).
- **`-race`** — race detector cần CGO và một trình biên dịch C, máy dev không có gcc.
  Hai runner Unix chạy thay.

Code không bao giờ được chạy thì hỏng lúc nào không hay, mà cả hai chỗ này lại nằm đúng
phần khó nhất: đồng thời và durability.

## Đã có gì

| Phần | Trạng thái |
|---|---|
| KV in-memory, an toàn đa luồng | xong — [02](02-in-memory-store.md) |
| Serialize entry thành byte | xong — [03](03-serialization.md) |
| Ghi log xuống đĩa, replay khi khởi động | xong — [04](04-write-ahead-log.md) |
| fsync: ghi thành công là chắc chắn không mất | xong — [04](04-write-ahead-log.md#đảm-bảo-dữ-liệu-thật-sự-xuống-đĩa) |
| Checksum + khôi phục sau khi mất điện | xong — [04](04-write-ahead-log.md#ghi-dở-torn-write) |
| Server + giao thức để client kết nối | chưa |
| Compaction, on-disk format, index | chưa |

Tới đây database đã có đủ **log + checksum + fsync**, tức là phần lõi: khởi động lại sau
khi mất điện vẫn giữ nguyên mọi lần ghi đã báo thành công.

## Lộ trình

1. **Server + giao thức.** Cho client nói chuyện với db qua network, thay vì chỉ gọi hàm
   trong cùng process.
2. **Compaction.** Log chỉ nối thêm nên nó phình mãi; key ghi đè 1000 lần thì có 1000 bản
   ghi mà chỉ 1 bản còn giá trị. Cần dọn định kỳ.
3. **On-disk format + index.** Bỏ giả định "toàn bộ dữ liệu vừa trong RAM". Khi có cấu
   trúc dữ liệu trên đĩa thì atomicity và durability phải tính lại từ đầu — log một record
   là chuyện dễ, giữ nguyên tính nhất quán của cả một B-tree lại là chuyện khác.

## Giới hạn hiện tại

Phần durability đã xong, nhưng **chưa kết nối được từ bên ngoài** — vẫn phải gọi hàm
trong cùng process, chưa có server. Đó là bước 1 của lộ trình.

Tốc độ ghi hiện rất thấp (~2.900 ghi/giây) vì fsync sau mỗi record. Đó là lựa chọn có chủ
ý: an toàn trước, nhanh sau.

Và một điểm yếu còn lại: hỏng dữ liệu ở **giữa** log làm mất mọi record phía sau, không
chỉ record hỏng. Format hiện tại không tránh được — xem
[04](04-write-ahead-log.md#cái-checksum-không-làm-được).
