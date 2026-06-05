import type {ReactNode} from 'react';
import clsx from 'clsx';
import Heading from '@theme/Heading';
import styles from './styles.module.css';

type FeatureItem = {
  title: string;
  Svg: React.ComponentType<React.ComponentProps<'svg'>>;
  description: ReactNode;
};

const FeatureList: FeatureItem[] = [
  {
    title: '無 GC',
    Svg: require('@site/static/img/0.svg').default,
    description: (
      <>
        不依賴垃圾回收，自动安全內存管理
      </>
    ),
  },
  {
    title: '內存安全',
    Svg: require('@site/static/img/0.svg').default,
    description: (
      <>
        作用域離開自動釋放，杜絕懸垂引用、內存泄漏
      </>
    ),
  },
  {
    title: '語法極簡',
    Svg: require('@site/static/img/0.svg').default,
    description: (
      <>
        減少關鍵字，無冗余語法
      </>
    ),
  },
];

function Feature({title, Svg, description}: FeatureItem) {
  return (
    <div className={clsx('col col--4')}>
      <div className="text--center">
        <Svg className={styles.featureSvg} role="img" />
      </div>
      <div className="text--center padding-horiz--md">
        <Heading as="h3">{title}</Heading>
        <p>{description}</p>
      </div>
    </div>
  );
}

export default function HomepageFeatures(): ReactNode {
  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {FeatureList.map((props, idx) => (
            <Feature key={idx} {...props} />
          ))}
        </div>
      </div>
    </section>
  );
}
