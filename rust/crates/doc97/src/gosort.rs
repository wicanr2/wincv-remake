//! Go 1.22 `sort.Slice`(pdqsort_func)逐行照搬。
//!
//! `sort.Slice` 不是穩定排序:鍵相同的元素最後排成什麼次序,取決於演算法的
//! 每一步。段落與字元屬性表依 FC 起點排序後要用二分搜尋查,起點相同的兩筆
//! 誰在前面會決定查到哪一筆,所以要與 Go 排出同一個次序,不能換成 Rust 的
//! `sort_unstable_by`。連 Go 版 partialInsertionSort 裡那個用絕對索引 1
//! (而不是 a+1)當下界的寫法也照抄。

fn bits_len(x: usize) -> u32 {
    usize::BITS - x.leading_zeros()
}

struct Data<'a, T, F: Fn(&T, &T) -> bool> {
    v: &'a mut [T],
    less: F,
}

impl<T, F: Fn(&T, &T) -> bool> Data<'_, T, F> {
    fn less(&self, i: usize, j: usize) -> bool {
        (self.less)(&self.v[i], &self.v[j])
    }
    fn swap(&mut self, i: usize, j: usize) {
        self.v.swap(i, j);
    }
}

#[derive(PartialEq, Eq, Clone, Copy)]
enum Hint {
    Unknown,
    Increasing,
    Decreasing,
}

/// Go 的 `sort.Slice(x, less)`。
pub fn slice<T>(v: &mut [T], less: impl Fn(&T, &T) -> bool) {
    let n = v.len();
    let limit = bits_len(n) as i64;
    let mut d = Data { v, less };
    pdqsort(&mut d, 0, n, limit);
}

fn insertion_sort<T, F: Fn(&T, &T) -> bool>(d: &mut Data<T, F>, a: usize, b: usize) {
    for i in a + 1..b {
        let mut j = i;
        while j > a && d.less(j, j - 1) {
            d.swap(j, j - 1);
            j -= 1;
        }
    }
}

fn sift_down<T, F: Fn(&T, &T) -> bool>(d: &mut Data<T, F>, lo: usize, hi: usize, first: usize) {
    let mut root = lo;
    loop {
        let mut child = 2 * root + 1;
        if child >= hi {
            break;
        }
        if child + 1 < hi && d.less(first + child, first + child + 1) {
            child += 1;
        }
        if !d.less(first + root, first + child) {
            return;
        }
        d.swap(first + root, first + child);
        root = child;
    }
}

fn heap_sort<T, F: Fn(&T, &T) -> bool>(d: &mut Data<T, F>, a: usize, b: usize) {
    let first = a;
    let lo = 0;
    let hi = b - a;
    let mut i = (hi as i64 - 1) / 2;
    while i >= 0 {
        sift_down(d, i as usize, hi, first);
        i -= 1;
    }
    let mut i = hi as i64 - 1;
    while i >= 0 {
        d.swap(first, first + i as usize);
        sift_down(d, lo, i as usize, first);
        i -= 1;
    }
}

fn pdqsort<T, F: Fn(&T, &T) -> bool>(d: &mut Data<T, F>, mut a: usize, mut b: usize, mut limit: i64) {
    const MAX_INSERTION: usize = 12;
    let mut was_balanced = true;
    let mut was_partitioned = true;
    loop {
        let length = b - a;
        if length <= MAX_INSERTION {
            insertion_sort(d, a, b);
            return;
        }
        if limit == 0 {
            heap_sort(d, a, b);
            return;
        }
        if !was_balanced {
            break_patterns(d, a, b);
            limit -= 1;
        }
        let (mut pivot, mut hint) = choose_pivot(d, a, b);
        if hint == Hint::Decreasing {
            reverse_range(d, a, b);
            pivot = (b - 1) - (pivot - a);
            hint = Hint::Increasing;
        }
        if was_balanced && was_partitioned && hint == Hint::Increasing && partial_insertion_sort(d, a, b) {
            return;
        }
        if a > 0 && !d.less(a - 1, pivot) {
            let mid = partition_equal(d, a, b, pivot);
            a = mid;
            continue;
        }
        let (mid, already) = partition(d, a, b, pivot);
        was_partitioned = already;
        let (left, right) = (mid - a, b - mid);
        let balance = length / 8;
        if left < right {
            was_balanced = left >= balance;
            pdqsort(d, a, mid, limit);
            a = mid + 1;
        } else {
            was_balanced = right >= balance;
            pdqsort(d, mid + 1, b, limit);
            b = mid;
        }
    }
}

fn partition<T, F: Fn(&T, &T) -> bool>(d: &mut Data<T, F>, a: usize, b: usize, pivot: usize) -> (usize, bool) {
    d.swap(a, pivot);
    // i 與 j 可以退到 a 之下一格,用有號數。
    let (mut i, mut j) = (a as i64 + 1, b as i64 - 1);
    while i <= j && d.less(i as usize, a) {
        i += 1;
    }
    while i <= j && !d.less(j as usize, a) {
        j -= 1;
    }
    if i > j {
        d.swap(j as usize, a);
        return (j as usize, true);
    }
    d.swap(i as usize, j as usize);
    i += 1;
    j -= 1;
    loop {
        while i <= j && d.less(i as usize, a) {
            i += 1;
        }
        while i <= j && !d.less(j as usize, a) {
            j -= 1;
        }
        if i > j {
            break;
        }
        d.swap(i as usize, j as usize);
        i += 1;
        j -= 1;
    }
    d.swap(j as usize, a);
    (j as usize, false)
}

fn partition_equal<T, F: Fn(&T, &T) -> bool>(d: &mut Data<T, F>, a: usize, b: usize, pivot: usize) -> usize {
    d.swap(a, pivot);
    let (mut i, mut j) = (a as i64 + 1, b as i64 - 1);
    loop {
        while i <= j && !d.less(a, i as usize) {
            i += 1;
        }
        while i <= j && d.less(a, j as usize) {
            j -= 1;
        }
        if i > j {
            break;
        }
        d.swap(i as usize, j as usize);
        i += 1;
        j -= 1;
    }
    i as usize
}

fn partial_insertion_sort<T, F: Fn(&T, &T) -> bool>(d: &mut Data<T, F>, a: usize, b: usize) -> bool {
    const MAX_STEPS: usize = 5;
    const SHORTEST_SHIFTING: usize = 50;
    let mut i = a + 1;
    for _ in 0..MAX_STEPS {
        while i < b && !d.less(i, i - 1) {
            i += 1;
        }
        if i == b {
            return true;
        }
        if b - a < SHORTEST_SHIFTING {
            return false;
        }
        d.swap(i, i - 1);
        if i - a >= 2 {
            let mut j = i - 1;
            while j >= 1 {
                if !d.less(j, j - 1) {
                    break;
                }
                d.swap(j, j - 1);
                j -= 1;
            }
        }
        if b - i >= 2 {
            let mut j = i + 1;
            while j < b {
                if !d.less(j, j - 1) {
                    break;
                }
                d.swap(j, j - 1);
                j += 1;
            }
        }
    }
    false
}

fn break_patterns<T, F: Fn(&T, &T) -> bool>(d: &mut Data<T, F>, a: usize, b: usize) {
    let length = b - a;
    if length >= 8 {
        let mut random = length as u64;
        let modulus = 1u64 << bits_len(length);
        let mut idx = a + (length / 4) * 2 - 1;
        while idx <= a + (length / 4) * 2 + 1 {
            random ^= random << 13;
            random ^= random >> 17;
            random ^= random << 5;
            let mut other = (random & (modulus - 1)) as usize;
            if other >= length {
                other -= length;
            }
            d.swap(idx, a + other);
            idx += 1;
        }
    }
}

fn choose_pivot<T, F: Fn(&T, &T) -> bool>(d: &mut Data<T, F>, a: usize, b: usize) -> (usize, Hint) {
    const SHORTEST_NINTHER: usize = 50;
    const MAX_SWAPS: i32 = 4 * 3;
    let l = b - a;
    let mut swaps = 0;
    let mut i = a + l / 4;
    let mut j = a + l / 4 * 2;
    let mut k = a + l / 4 * 3;
    if l >= 8 {
        if l >= SHORTEST_NINTHER {
            i = median(d, i - 1, i, i + 1, &mut swaps);
            j = median(d, j - 1, j, j + 1, &mut swaps);
            k = median(d, k - 1, k, k + 1, &mut swaps);
        }
        j = median(d, i, j, k, &mut swaps);
    }
    match swaps {
        0 => (j, Hint::Increasing),
        MAX_SWAPS => (j, Hint::Decreasing),
        _ => (j, Hint::Unknown),
    }
}

fn order2<T, F: Fn(&T, &T) -> bool>(d: &Data<T, F>, a: usize, b: usize, swaps: &mut i32) -> (usize, usize) {
    if d.less(b, a) {
        *swaps += 1;
        return (b, a);
    }
    (a, b)
}

fn median<T, F: Fn(&T, &T) -> bool>(d: &Data<T, F>, a: usize, b: usize, c: usize, swaps: &mut i32) -> usize {
    let (a, b) = order2(d, a, b, swaps);
    let (b, c) = order2(d, b, c, swaps);
    let _ = c;
    let (_a, b) = order2(d, a, b, swaps);
    b
}

fn reverse_range<T, F: Fn(&T, &T) -> bool>(d: &mut Data<T, F>, a: usize, b: usize) {
    let (mut i, mut j) = (a, b - 1);
    while i < j {
        d.swap(i, j);
        i += 1;
        j -= 1;
    }
}

#[cfg(test)]
mod tests {
    /// 鍵相同時不穩定:排出來的次序要與 Go 一樣。`gosort_go.txt` 是 Go 1.22.12
    /// 對同一組輸入(線性同餘產生的 0–6 鍵,值是原位置)跑 sort.Slice 的結果。
    #[test]
    fn same_order_as_go() {
        for line in include_str!("gosort_go.txt").lines() {
            let (n, want) = line.split_once(':').unwrap();
            let n: usize = n.parse().unwrap();
            let mut v: Vec<(u32, usize)> = Vec::with_capacity(n);
            let mut x: u32 = 12345;
            for i in 0..n {
                x = x.wrapping_mul(1103515245).wrapping_add(12345);
                v.push(((x >> 16) % 7, i));
            }
            super::slice(&mut v, |a, b| a.0 < b.0);
            let got: Vec<String> = v.iter().map(|e| e.1.to_string()).collect();
            assert_eq!(got.join(" "), want.trim(), "n={n}");
        }
    }
}
