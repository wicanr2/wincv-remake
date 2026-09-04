package tw.lcy.wincv;

import java.util.Locale;
import android.app.Activity;
import android.os.Build;
import android.os.Bundle;
import android.os.Environment;

import go.Seq;

import tw.lcy.wincv.mobile.EbitenView;
import tw.lcy.wincv.mobile.Mobile;

/**
 * WinCV Remake 的 Android 進入點。
 *
 * 這一層只做三件事:告訴 Go 要瀏覽哪個目錄、把 EbitenView 顯示出來、
 * 轉發生命週期。畫面與按鍵分派全在 Go 那一側(internal/app),
 * 與桌面版共用同一份程式碼。
 */
public class MainActivity extends Activity {

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        // gomobile 產生的原生層要有 Context 才能問 Android 拿東西,而這一步
        // 要由 app 自己做 —— 沒人做的話原生層的 context 是 NULL。
        //
        // 症狀離原因很遠:Ebiten 拿不到 DisplayMetrics,deviceScale 就是 0;
        // EbitenView.onLayout 用「像素 ÷ deviceScale」算版面尺寸,除出來是
        // +Inf;Ebiten 再用它乘上 0 去算畫布大小,得到 NaN,轉成 int 是
        // INT64_MIN,最後死在「NewImage 的寬必須是正數」。
        // 中間沒有任何一步提到 Context。
        Seq.setContext(getApplicationContext());

        // Go 那一側猜不出自己讀得到哪裡 —— Android 10 起是 scoped storage,
        // 能讀什麼是由這一層的授權決定的,所以要明講。
        String root = Environment.getExternalStorageDirectory().getAbsolutePath();
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R
                && !Environment.isExternalStorageManager()) {
            // 沒有「所有檔案存取權」時退到 app 自己的目錄,
            // 至少看得到東西,而不是一片空白。
            java.io.File f = getExternalFilesDir(null);
            if (f != null) {
                root = f.getAbsolutePath();
            }
        }
        // 語系在 Resources 裡,Go 那一側讀不到(Android 沒有 LANG)。
        // 要在 setRoot 之前送,畫面第一幀就會是對的語言。
        Mobile.setLocale(Locale.getDefault().toLanguageTag());
        Mobile.setRoot(root);

        setContentView(R.layout.activity_main);
    }

    private EbitenView getEbitenView() {
        return (EbitenView) this.findViewById(R.id.ebitenview);
    }

    @Override
    protected void onPause() {
        super.onPause();
        getEbitenView().suspendGame();
    }

    @Override
    protected void onResume() {
        super.onResume();
        getEbitenView().resumeGame();
    }
}
