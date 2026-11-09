//! Go 1.22 `sort.SliceStable` 的演算法(`sort/zsortfunc.go` 的 stable_func),
//! 逐行照搬。
//!
//! 為什麼不用 `slice::sort_by`:Go 版遞減排序的 less 是 `!less`,遇到
//! 「名字只差大小寫」的兩項時兩個方向都回 true,比較函式前後矛盾。
//! Go 的演算法遇到這種情況會把那一組的先後顛倒;Rust 的排序要求全序,
//! 矛盾時結果不同(甚至可能 panic)。要與 Go 版的檔案列表逐項相同,
//! 只能用同一個演算法、餵同一個 less。

/// 以 Go 的穩定排序演算法排序。`less(a, b)` 的語意與 Go 相同。
pub fn slice_stable<T>(v: &mut [T], less: impl Fn(&T, &T) -> bool) {
    let n = v.len();
    let lt = |v: &[T], i: usize, j: usize| less(&v[i], &v[j]);
    let mut block = 20;
    let (mut a, mut b) = (0, block);
    while b <= n {
        insertion(v, a, b, &lt);
        a = b;
        b += block;
    }
    insertion(v, a, n, &lt);
    while block < n {
        a = 0;
        b = 2 * block;
        while b <= n {
            sym_merge(v, a, a + block, b, &lt);
            a = b;
            b += 2 * block;
        }
        let m = a + block;
        if m < n {
            sym_merge(v, a, m, n, &lt);
        }
        block *= 2;
    }
}

fn insertion<T>(v: &mut [T], a: usize, b: usize, lt: &impl Fn(&[T], usize, usize) -> bool) {
    for i in a + 1..b {
        let mut j = i;
        while j > a && lt(v, j, j - 1) {
            v.swap(j, j - 1);
            j -= 1;
        }
    }
}

fn sym_merge<T>(v: &mut [T], a: usize, m: usize, b: usize, lt: &impl Fn(&[T], usize, usize) -> bool) {
    if m - a == 1 {
        let (mut i, mut j) = (m, b);
        while i < j {
            let h = (i + j) >> 1;
            if lt(v, h, a) {
                i = h + 1;
            } else {
                j = h;
            }
        }
        let mut k = a;
        while k + 1 < i {
            v.swap(k, k + 1);
            k += 1;
        }
        return;
    }
    if b - m == 1 {
        let (mut i, mut j) = (a, m);
        while i < j {
            let h = (i + j) >> 1;
            if !lt(v, m, h) {
                i = h + 1;
            } else {
                j = h;
            }
        }
        let mut k = m;
        while k > i {
            v.swap(k, k - 1);
            k -= 1;
        }
        return;
    }
    let mid = (a + b) >> 1;
    let n = mid + m;
    let (mut start, mut r) = if m > mid { (n - b, mid) } else { (a, m) };
    let p = n - 1;
    while start < r {
        let c = (start + r) >> 1;
        if !lt(v, p - c, c) {
            start = c + 1;
        } else {
            r = c;
        }
    }
    let end = n - start;
    if start < m && m < end {
        rotate(v, start, m, end);
    }
    if a < start && start < mid {
        sym_merge(v, a, start, mid, lt);
    }
    if mid < end && end < b {
        sym_merge(v, mid, end, b, lt);
    }
}

fn rotate<T>(v: &mut [T], a: usize, m: usize, b: usize) {
    let (mut i, mut j) = (m - a, b - m);
    while i != j {
        if i > j {
            swap_range(v, m - i, m, j);
            i -= j;
        } else {
            swap_range(v, m - i, m + j - i, i);
            j -= i;
        }
    }
    swap_range(v, m - i, m, i);
}

fn swap_range<T>(v: &mut [T], a: usize, b: usize, n: usize) {
    for i in 0..n {
        v.swap(a + i, b + i);
    }
}
